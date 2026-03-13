BINARY_NAME=petsciiproxy

build:	
	GOOS=darwin GOARCH=arm64 go build -o ./dist/${BINARY_NAME}-apple-silicon main.go  
	GOOS=windows GOARCH=amd64 go build -o ./dist/${BINARY_NAME}-windows.exe main.go
