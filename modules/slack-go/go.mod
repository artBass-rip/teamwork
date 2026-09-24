module teamwork/modules/slack

go 1.26

require (
	github.com/gorilla/websocket v1.5.3
	teamwork/sdk/go v0.0.0
)

replace teamwork/sdk/go => ../../sdk/go
