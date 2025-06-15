.PHONY: build run test clean docker-build docker-run

build:
	go build -o ./build/ctlstream ./cmd/ctlstream

run: build
	./build/ctlstream

docker-build:
	docker build -t ctlstream:latest .

docker-run: docker-build
	docker run -p 8080:8080 ctlstream:latest