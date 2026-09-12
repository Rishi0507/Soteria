module soteria/services/notification-service

go 1.26.0

require (
	github.com/joho/godotenv v1.5.1
	modernc.org/sqlite v1.58.0
	soteria/libs/core v0.0.0
	soteria/libs/feedkit v0.0.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/rabbitmq/amqp091-go v1.14.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace soteria/libs/core => ../../libs/core

replace soteria/libs/feedkit => ../../libs/feedkit
