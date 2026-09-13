// Package tsa anchors a hash in time with an RFC 3161 timestamp authority.
//
// A hash chain proves that a dossier is internally consistent, but it cannot
// prove *when* it was made: whoever holds the data could recompute the whole
// chain after the fact. A timestamp authority signs the chain head, which is
// what turns "these events are consistent" into "these events existed by this
// date", and that is the claim an insurer or a regulator actually relies on.
//
// The token is stored opaquely. Verification is the authority's job, and a
// recipient checks it with their own tools rather than trusting ours.
package tsa

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"
)

// DefaultURL is a free public authority, usable without an account.
const DefaultURL = "http://timestamp.digicert.com"

// sha256OID identifies the digest algorithm in the request.
var sha256OID = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}

// Client talks to one timestamp authority.
type Client struct {
	URL  string
	HTTP *http.Client
}

func New(url string) *Client {
	if url == "" {
		url = DefaultURL
	}
	return &Client{URL: url, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// request is the RFC 3161 TimeStampReq.
type request struct {
	Version        int
	MessageImprint messageImprint
	Nonce          *big.Int `asn1:"optional"`
	CertReq        bool     `asn1:"optional,default:false"`
}

type messageImprint struct {
	HashAlgorithm algorithmIdentifier
	HashedMessage []byte
}

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

// response is the TimeStampResp. The token itself is kept as raw DER: parsing a
// CMS SignedData to re-verify it here would duplicate work the recipient must do
// with their own trusted roots anyway.
type response struct {
	Status statusInfo
	Token  asn1.RawValue `asn1:"optional"`
}

type statusInfo struct {
	Status       int
	StatusString []string       `asn1:"optional"`
	FailInfo     asn1.BitString `asn1:"optional"`
}

// Stamp submits a digest and returns the authority's token, base64-encoded.
func (c *Client) Stamp(ctx context.Context, digest []byte) (string, error) {
	if len(digest) != 32 {
		return "", fmt.Errorf("tsa: expected a 32-byte sha256 digest, got %d bytes", len(digest))
	}

	nonce, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return "", fmt.Errorf("tsa: nonce: %w", err)
	}

	body, err := asn1.Marshal(request{
		Version: 1,
		MessageImprint: messageImprint{
			HashAlgorithm: algorithmIdentifier{
				Algorithm:  sha256OID,
				Parameters: asn1.RawValue{Tag: asn1.TagNull},
			},
			HashedMessage: digest,
		},
		Nonce: nonce,
		// Ask for the signing certificate: a token the recipient cannot trace to
		// a certificate is not much use to them.
		CertReq: true,
	})
	if err != nil {
		return "", fmt.Errorf("tsa: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/timestamp-query")
	req.Header.Set("Accept", "application/timestamp-reply")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("tsa: %s: %w", c.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tsa: %s: HTTP %d", c.URL, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("tsa: read reply: %w", err)
	}

	var out response
	if _, err := asn1.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("tsa: decode reply: %w", err)
	}
	// 0 granted, 1 granted with modifications; anything else is a refusal.
	if out.Status.Status > 1 {
		return "", fmt.Errorf("tsa: refused (status %d): %v", out.Status.Status, out.Status.StatusString)
	}
	if len(out.Token.FullBytes) == 0 {
		return "", fmt.Errorf("tsa: reply carried no token")
	}
	return base64.StdEncoding.EncodeToString(out.Token.FullBytes), nil
}
