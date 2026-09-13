import { http, HttpResponse, passthrough } from 'msw'

const RESOLUTION = 'http://localhost:8081'
const RESCUE = 'http://localhost:8083'

const rescue = {
    rescue_id: 'r-mock-1',
    incident_id: 'inc-fda_enforcement-f-2291-2026',
    order_id: 'ORD-1001',
    status: 'PROPOSED',
    hazard: 'Undeclared peanut',
    proposed_at: '2026-09-12T10:00:00Z',
    expires_at: '2026-09-14T10:00:00Z',
    affected_line: {
        line_item_id: 'li-1',
        gtin: '00041196910537',
        sku: 'SF-GB-12',
        product_title: 'Sunfield Farms Chewy Granola Bars 12 ct',
        lot_code: '8H-1132',
        quantity: 2,
        unit_price: { amount_minor: 449, currency: 'USD' },
    },
    options: [
        {
            option_id: 'sub-00012345678905',
            kind: 'SUBSTITUTE',
            gtin: '00012345678905',
            product_title: 'Harvest Lane Oat and Honey Granola Bars 12 ct',
            unit_price: { amount_minor: 449, currency: 'USD' },
            allergen_safe: true,
            allergens: ['gluten'],
            rationale: 'same price, complete allergen data, no recall hazard allergen',
        },
        { option_id: 'refund', kind: 'REFUND', rationale: 'Refund this item and keep the rest of the order.' },
        { option_id: 'cancel', kind: 'CANCEL', rationale: 'Cancel the whole order.' },
    ],
}

export const handlers = [
    http.get(`${RESOLUTION}/v1/lots/status`, ({ request }) => {
        const url = new URL(request.url)
        const gtin = url.searchParams.get('gtin') ?? ''
        const lot = url.searchParams.get('lot_code') ?? undefined

        // Real recalled lot codes from the seeded catalog (Sept 2026 FDA notices)
        // plus the original fixture code, so the demo store can show a hit.
        const RECALLED: Record<string, string> = {
            L2408B: 'Undeclared peanut',
            LB028ACP04: 'Salmonella — Botticelli Foods / bettergoods Pistachio Nut Butter (FDA H-1273-2026)',
            LLA618203: 'Foreign material (glass pieces) — Outshine Fruit Bars (FDA H-1265-2026)',
            '1226183': 'Undeclared fish — Sun Noodle Sura Tanmen (FDA H-1258-2026)',
            '8H-1132': 'Listeria monocytogenes',
            'P-1950': 'Salmonella Enteritidis — Kroger Grade A eggs (FDA H-1230-2026)',
        }
        if (lot && RECALLED[lot]) {
            return HttpResponse.json({
                gtin,
                lot_code: lot,
                verdict: 'AFFECTED',
                incident_id: 'INC-2026-0412',
                hazard: RECALLED[lot],
                confidence: 0.97,
                checked_at: new Date().toISOString(),
            })
        }

        if (lot === 'L9999Z') {
            return HttpResponse.json({
                gtin,
                lot_code: lot,
                verdict: 'UNKNOWN_LOT',
                checked_at: new Date().toISOString(),
            })
        }

        if (lot === 'BOOM') {
            return new HttpResponse(null, { status: 500 })
        }

        return HttpResponse.json({
            gtin,
            lot_code: lot,
            verdict: 'SAFE',
            checked_at: new Date().toISOString(),
        })
    }),
    http.get('https://world.openfoodfacts.org/api/v2/product/:gtin', ({ params }) => {
        const gtin = params.gtin as string

        // Only the fixture barcodes are mocked; real barcodes go to the real
        // Open Food Facts API so the store shows live allergen data.
        if (!['0000000000000', '1111111111111', '00041196910537', '041196910537'].includes(gtin)) {
            return passthrough()
        }

        if (gtin === '0000000000000') {
            return HttpResponse.json({ status: 0 })
        }

        if (gtin === '1111111111111') {
            return HttpResponse.json({
                status: 1,
                product: {
                    product_name: 'Unknown Brand Crackers',
                    ingredients_text: 'Wheat flour, vegetable oil, salt',
                },
            })
        }

        return HttpResponse.json({
            status: 1,
            product: {
                product_name: 'Nutella',
                brands: 'Nutella, Ferrero',
                allergens_tags: ['en:milk', 'en:nuts', 'en:soybeans'],
                traces_tags: [],
                ingredients_text: 'Sugar, palm oil, hazelnuts 13%, skimmed milk powder, cocoa',
            },
        })
    }),
    http.get(`${RESCUE}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'order-rescue-service', bus: 'up' })
    ),
    http.get(`${RESOLUTION}/healthz`, () =>
        HttpResponse.json({ status: 'ok', service: 'resolution-service', bus: 'up' })
    ),
    http.get(`${RESCUE}/v1/rescues`, ({ request }) => {
        const orderId = new URL(request.url).searchParams.get('order_id')
        // ORD-1002 holds a clean lot: nothing to rescue, and the API says so
        // with an empty list rather than an error.
        if (orderId && orderId !== 'ORD-1001') {
            return HttpResponse.json({ items: [] })
        }
        return HttpResponse.json({ items: [rescue] })
    }),
    http.get(`${RESCUE}/v1/rescues/:rescueId`, ({ params }) =>
        HttpResponse.json({ ...rescue, rescue_id: params.rescueId as string })
    ),
    http.post(`${RESCUE}/v1/rescues/:rescueId/confirm`, async ({ request, params }) => {
        const body = (await request.json()) as { option_id?: string; consent_token?: string }
        // The real service refuses a confirmation without the token it issued;
        // the mock refuses too, so the UI's 403 path is exercised offline.
        if (!body.consent_token || body.consent_token === 'forged') {
            return HttpResponse.json(
                { code: 'invalid_consent', message: 'consent token does not match this rescue' },
                { status: 403 }
            )
        }
        return HttpResponse.json({
            ...rescue,
            rescue_id: params.rescueId as string,
            status: 'CONFIRMED',
            chosen_option_id: body.option_id,
            confirmed_at: new Date().toISOString(),
        })
    }),
]