import { GraphQLClient } from 'graphql-request'

const domain = import.meta.env.VITE_SHOPIFY_DOMAIN
const token = import.meta.env.VITE_SHOPIFY_STOREFRONT_TOKEN
const version = import.meta.env.VITE_SHOPIFY_API_VERSION

if (!domain || !token || !version) {
    throw new Error(
        'Missing Shopify env vars. Check apps/storefront/.env.local against .env.example'
    )
}

export const shopify = new GraphQLClient(
    `https://${domain}/api/${version}/graphql.json`,
    { headers: { 'X-Shopify-Storefront-Access-Token': token } }
)