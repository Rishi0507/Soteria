module soteria/tools/demo

go 1.26.0

require (
	soteria/libs/core v0.0.0
	soteria/libs/shopify v0.0.0
	soteria/services/containment-service v0.0.0
	soteria/services/order-rescue-service v0.0.0
	soteria/services/resolution-service v0.0.0
)

require github.com/rabbitmq/amqp091-go v1.14.0 // indirect

replace (
	soteria/libs/core => ../../libs/core
	soteria/libs/shopify => ../../libs/shopify
	soteria/services/containment-service => ../../services/containment-service
	soteria/services/order-rescue-service => ../../services/order-rescue-service
	soteria/services/resolution-service => ../../services/resolution-service
)
