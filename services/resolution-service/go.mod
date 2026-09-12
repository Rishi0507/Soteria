module soteria/services/resolution-service

go 1.26.0

require (
	soteria/libs/core v0.0.0
	soteria/libs/shopify v0.0.0
)

require github.com/rabbitmq/amqp091-go v1.14.0 // indirect

replace (
	soteria/libs/core => ../../libs/core
	soteria/libs/shopify => ../../libs/shopify
)
