import { http, HttpResponse } from 'msw'

const RESOLUTION = 'http://localhost:8081'

export const handlers = [
    http.get(`${RESOLUTION}/v1/lots/status`, ({ request }) => {
        const url = new URL(request.url)
        const gtin = url.searchParams.get('gtin') ?? ''
        const lot = url.searchParams.get('lot_code') ?? undefined

        if (lot === 'L2408B') {
            return HttpResponse.json({
                gtin,
                lot_code: lot,
                verdict: 'AFFECTED',
                incident_id: 'INC-2026-0412',
                hazard: 'Undeclared peanut',
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
]