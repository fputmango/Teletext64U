BINARY_NAME=petsciiproxy

build:	
	GOOS=darwin GOARCH=arm64 go build -o ./dist/${BINARY_NAME}-mac-silicon main.go  
	GOOS=darwin GOARCH=amd64 go build -o ./dist/${BINARY_NAME}-mac-intel main.go
	GOOS=windows GOARCH=amd64 go build -o ./dist/${BINARY_NAME}-windows.exe main.go
	GOOS=linux GOARCH=386 go build -o ./dist/${BINARY_NAME}-linux-386 main.go
