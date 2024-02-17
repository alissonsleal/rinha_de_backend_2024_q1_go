dev:
	nodemon --exec go run ./src/main.go --signal SIGTERM
build:
	go build -o ./bin/main ./src/main.go
run:
	./bin/main
test:
	go test -v ./src/...
clean:
	rm -rf ./bin
docker-start:
	docker-compose up --build