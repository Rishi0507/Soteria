package notify

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"soteria/libs/core/events"
)

// Router decides who is told what for each event type.
type Router struct {
	// OpsRecipient is the logical recipient for retailer-side alerts (the
	// Slack webhook is bound to a channel already; this is just the label).
	OpsRecipient string
	// ConsentURL is the storefront page where the customer chooses a rescue
	// option; "{rescue_id}" is substituted. Empty disables the link.
	ConsentURL string
	// RetailerName appears in customer-facing copy.
	RetailerName string
}

// Route returns the messages an event should produce. An unknown event type
// yields nil, nil (ignored, acked).
func (r *Router) Route(env events.Envelope) ([]Message, error) {
	switch env.EventType {
	case events.TypeContainmentTaken:
		var p events.ContainmentActionTaken
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, Permanent(fmt.Errorf("decode %s: %w", env.EventType, err))
		}
		return []Message{r.containmentTaken(p)}, nil
	case events.TypeOrderRescueProposed:
		var p events.OrderRescueProposed
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, Permanent(fmt.Errorf("decode %s: %w", env.EventType, err))
		}
		return r.rescueProposed(p), nil
	case events.TypeEvasionFlagged:
		var p events.EvasionFlagged
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, Permanent(fmt.Errorf("decode %s: %w", env.EventType, err))
		}
		return []Message{r.evasionFlagged(p)}, nil
	}
	return nil, nil
}

// IncidentID extracts the incident from any of the routed payloads.
func IncidentID(env events.Envelope) string {
	if env.CorrelationID != "" {
		return env.CorrelationID
	}
	var probe struct {
		IncidentID string `json:"incident_id"`
	}
	_ = json.Unmarshal(env.Payload, &probe)
	return probe.IncidentID
}

// ---- retailer ops (Slack) ---------------------------------------------------

func (r *Router) containmentTaken(p events.ContainmentActionTaken) Message {
	var held, sellable, failed int
	var lines []string
	for _, res := range p.Results {
		title := res.GTIN
		for _, t := range p.Targets {
			if t.GTIN == res.GTIN && t.ProductTitle != "" {
				title = fmt.Sprintf("%s (%s)", t.ProductTitle, res.GTIN)
			}
		}
		switch res.Status {
		case "HELD":
			held += res.UnitsHeld
			sellable += res.UnitsLeftSellable
			lines = append(lines, fmt.Sprintf("• ✅ *%s* — %d units quarantined, %d left sellable", title, res.UnitsHeld, res.UnitsLeftSellable))
		case "FAILED":
			failed++
			lines = append(lines, fmt.Sprintf("• ❌ *%s* — hold FAILED: %s", title, res.Error))
		case "RELEASED":
			lines = append(lines, fmt.Sprintf("• ↩️ *%s* — released back to sale", title))
		default:
			lines = append(lines, fmt.Sprintf("• ⏭️ *%s* — %s", title, res.Status))
		}
	}
	lots := lotSummary(p.Targets)
	var subject string
	switch {
	case failed > 0:
		subject = fmt.Sprintf("⚠️ Containment partially FAILED — incident %s", p.IncidentID)
	case p.Decision == events.DecisionHumanRejected:
		subject = fmt.Sprintf("🚫 Containment rejected by %s — incident %s", p.Actor, p.IncidentID)
	case p.Decision == events.DecisionReleased:
		subject = fmt.Sprintf("↩️ Inventory released — incident %s", p.IncidentID)
	default:
		subject = fmt.Sprintf("🛑 Recall contained — incident %s", p.IncidentID)
	}
	text := fmt.Sprintf("*Hazard:* %s\n*Decision:* %s by %s (confidence %.2f, threshold %.2f)\n*Lots:* %s\n*Taken at:* %s\n\n%s\n\n_%d units quarantined, %d units still sellable._",
		orDash(p.Hazard), p.Decision, p.Actor, p.Confidence, p.Threshold, lots, p.TakenAt.UTC().Format(time.RFC3339), strings.Join(lines, "\n"), held, sellable)
	return Message{Channel: ChannelSlack, Recipient: r.OpsRecipient, Subject: subject, Text: text}
}

func (r *Router) evasionFlagged(p events.EvasionFlagged) Message {
	subject := fmt.Sprintf("🕵️ Recalled product resurfaced on %s — incident %s", p.Marketplace, p.IncidentID)
	text := fmt.Sprintf("*Listing:* <%s|%s>\n*Seller:* %s\n*GTIN / lot:* %s / %s\n*Confidence:* %.2f\n*Observed:* %s\n*Evidence:* %s",
		p.ListingURL, orDash(p.ListingTitle), orDash(p.Seller), orDash(p.GTIN), orDash(p.LotCode), p.Confidence,
		p.ObservedAt.UTC().Format(time.RFC3339), strings.Join(p.Evidence, "; "))
	return Message{Channel: ChannelSlack, Recipient: r.OpsRecipient, Subject: subject, Text: text}
}

// ---- customer (email / SMS) + ops copy --------------------------------------

func (r *Router) rescueProposed(p events.OrderRescueProposed) []Message {
	retailer := r.RetailerName
	if retailer == "" {
		retailer = "Your grocer"
	}
	link := ""
	if r.ConsentURL != "" {
		link = strings.ReplaceAll(r.ConsentURL, "{rescue_id}", p.RescueID)
	}
	product := p.AffectedLine.ProductTitle
	if product == "" {
		product = "item " + p.AffectedLine.GTIN
	}

	var opts []string
	var optsHTML []string
	for _, o := range p.Options {
		switch o.Kind {
		case events.OptionSubstitute:
			price := ""
			if o.UnitPrice != nil {
				price = fmt.Sprintf(" at %s (no extra charge)", money(*o.UnitPrice))
			}
			safe := ""
			if o.AllergenSafe {
				safe = " — checked allergen-safe"
			}
			opts = append(opts, fmt.Sprintf("Swap for %s%s%s", o.ProductTitle, price, safe))
			optsHTML = append(optsHTML, fmt.Sprintf("<li>Swap for <b>%s</b>%s%s</li>", html.EscapeString(o.ProductTitle), html.EscapeString(price), html.EscapeString(safe)))
		case events.OptionRefund:
			opts = append(opts, "Full refund for this item")
			optsHTML = append(optsHTML, "<li>Full refund for this item</li>")
		case events.OptionCancel:
			opts = append(opts, "Cancel this item from your order")
			optsHTML = append(optsHTML, "<li>Cancel this item from your order</li>")
		}
	}

	subject := fmt.Sprintf("Action needed: a recalled item is in your order %s", p.OrderID)
	text := fmt.Sprintf(`Hello,

%s has removed an item from your order %s because it is part of a food safety recall%s:

  %s  (lot %s, qty %d)

Nothing will change on your order until you choose what you would like us to do:

  %s
%s
This offer expires %s. Do not consume the recalled item; it can be returned for a refund at any time.

— %s food safety team`,
		retailer, p.OrderID, hazardClause(p.Hazard), product, orDash(p.AffectedLine.LotCode), p.AffectedLine.Quantity,
		strings.Join(opts, "\n  "), linkLine(link), p.ExpiresAt.UTC().Format("Mon 2 Jan 2006 15:04 UTC"), retailer)

	htmlBody := fmt.Sprintf(`<p>Hello,</p>
<p><b>%s</b> has removed an item from your order <b>%s</b> because it is part of a food safety recall%s:</p>
<p style="margin-left:1em">%s <br><small>lot %s · qty %d</small></p>
<p>Nothing will change on your order until you choose what you would like us to do:</p>
<ul>%s</ul>
%s
<p><small>This offer expires %s. Do not consume the recalled item; it can be returned for a refund at any time.</small></p>
<p>— %s food safety team</p>`,
		html.EscapeString(retailer), html.EscapeString(p.OrderID), html.EscapeString(hazardClause(p.Hazard)), html.EscapeString(product),
		html.EscapeString(orDash(p.AffectedLine.LotCode)), p.AffectedLine.Quantity, strings.Join(optsHTML, ""),
		linkHTML(link), p.ExpiresAt.UTC().Format("Mon 2 Jan 2006 15:04 UTC"), html.EscapeString(retailer))

	var out []Message
	switch {
	case p.Customer.Email != "":
		out = append(out, Message{Channel: ChannelEmail, Recipient: p.Customer.Email, Subject: subject, Text: text, HTML: htmlBody})
	case p.Customer.Phone != "":
		sms := fmt.Sprintf("%s: a recalled item (%s) is in your order %s. Choose a replacement or refund: %s", retailer, product, p.OrderID, link)
		out = append(out, Message{Channel: ChannelSMS, Recipient: p.Customer.Phone, Subject: subject, Text: sms})
	}
	// Ops always gets a copy so the retailer can follow up if the customer does not respond.
	out = append(out, Message{
		Channel:   ChannelSlack,
		Recipient: r.OpsRecipient,
		Subject:   fmt.Sprintf("📦 Order rescue proposed — order %s, incident %s", p.OrderID, p.IncidentID),
		Text: fmt.Sprintf("*Customer:* %s\n*Item:* %s (lot %s, qty %d)\n*Options offered:* %s\n*Expires:* %s\n*Customer notified via:* %s",
			Mask(firstNonEmpty(p.Customer.Email, p.Customer.Phone, p.Customer.CustomerID)), product, orDash(p.AffectedLine.LotCode), p.AffectedLine.Quantity,
			strings.Join(opts, " / "), p.ExpiresAt.UTC().Format(time.RFC3339), customerChannel(out)),
	})
	return out
}

func customerChannel(msgs []Message) string {
	for _, m := range msgs {
		if m.Channel != ChannelSlack {
			return m.Channel
		}
	}
	return "none (no contact details)"
}

func lotSummary(ts []events.ContainmentTarget) string {
	var parts []string
	for _, t := range ts {
		if len(t.LotCodes) > 0 {
			parts = append(parts, strings.Join(t.LotCodes, ", "))
		} else {
			parts = append(parts, "whole SKU "+t.GTIN)
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, "; ")
}

func hazardClause(h string) string {
	if h == "" {
		return ""
	}
	return " (" + h + ")"
}

func linkLine(link string) string {
	if link == "" {
		return ""
	}
	return "\nChoose here: " + link + "\n"
}

func linkHTML(link string) string {
	if link == "" {
		return ""
	}
	return fmt.Sprintf(`<p><a href="%s" style="display:inline-block;padding:10px 16px;background:#1a56db;color:#fff;border-radius:6px;text-decoration:none">Choose an option</a></p>`, html.EscapeString(link))
}

func money(m events.Money) string {
	return fmt.Sprintf("%s %d.%02d", m.Currency, m.AmountMinor/100, m.AmountMinor%100)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
