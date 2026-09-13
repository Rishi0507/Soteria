module soteria/tools/seedshop

go 1.26.0

require (
	soteria/libs/core v0.0.0
	soteria/libs/shopify v0.0.0
)

replace (
	soteria/libs/core => ../../libs/core
	soteria/libs/shopify => ../../libs/shopify
)
