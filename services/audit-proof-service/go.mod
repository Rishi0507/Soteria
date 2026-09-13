module soteria/services/audit-proof-service

go 1.26.0

require (
	github.com/go-pdf/fpdf v0.9.0
	soteria/libs/core v0.0.0
	soteria/libs/feedkit v0.0.0
)

require (
	github.com/rabbitmq/amqp091-go v1.14.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace (
	soteria/libs/core => ../../libs/core
	soteria/libs/feedkit => ../../libs/feedkit
)
