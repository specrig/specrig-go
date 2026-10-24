module example.com/shop

go 1.27.0

require (
	github.com/rabbitmq/amqp091-go v0.0.0
	gitlab.com/gorib/http v0.0.0
	gitlab.com/gorib/waffle v0.0.0
	google.golang.org/grpc v0.0.0
)

replace (
	github.com/rabbitmq/amqp091-go => ./fake/amqp091
	gitlab.com/gorib/http => ./fake/http
	gitlab.com/gorib/waffle => ./fake/waffle
	google.golang.org/grpc => ./fake/grpc
)
