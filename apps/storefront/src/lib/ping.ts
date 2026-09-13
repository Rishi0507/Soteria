import { shopify } from './shopify'

const SHOP_QUERY = `
  query {
    shop {
      name
      primaryDomain { url }
    }
  }
`

export async function ping() {
    const data = await shopify.request(SHOP_QUERY)
    console.log('Shopify connection OK:', data)
    return data
}