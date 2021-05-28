all : server client

server: src/server/main.go
	go build -o server src/server/main.go

client: src/client/main.go
	go build -o client src/client/main.go
