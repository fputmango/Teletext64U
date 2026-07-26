BINARY_NAME=petsciiproxy

dev:	
	GOOS=darwin GOARCH=arm64 go build -o ./dist/${BINARY_NAME}-mac-silicon .

build:	
	GOOS=darwin GOARCH=arm64 go build -o ./dist/${BINARY_NAME}-mac-silicon .
	GOOS=darwin GOARCH=amd64 go build -o ./dist/${BINARY_NAME}-mac-intel .
	GOOS=windows GOARCH=amd64 go build -o ./dist/${BINARY_NAME}-windows.exe .
	GOOS=linux GOARCH=386 go build -o ./dist/${BINARY_NAME}-linux-386 .
	GOOS=linux GOARCH=amd64 go build -o ./dist/${BINARY_NAME}-linux-64bit .
	GOOS=linux GOARCH=arm GOARM=7 go build -o ./dist/${BINARY_NAME}-raspberrypi-32bit .
	GOOS=linux GOARCH=arm GOARM=6 go build -o ./dist/${BINARY_NAME}-raspberrypi-1B .

# Upload to Development 
release-dev:
	@if [ -z "$(VPS_IP)" ]; then echo "Error: VPS_IP is not set!"; exit 1; fi
	GOOS=linux GOARCH=amd64 go build -o ${BINARY_NAME}-dev .
	scp ${BINARY_NAME}-dev $(VPS_USER)@$(VPS_IP):/tmp/${BINARY_NAME}-dev
	ssh $(VPS_USER)@$(VPS_IP) "mv /tmp/${BINARY_NAME}-dev /usr/local/bin/${BINARY_NAME}-dev && chmod +x /usr/local/bin/${BINARY_NAME}-dev && systemctl restart c64proxy-dev"
	rm ${BINARY_NAME}-dev

# Upload to Production 
release-prod:
	@if [ -z "$(VPS_IP)" ]; then echo "Error: VPS_IP is not set!"; exit 1; fi
	GOOS=linux GOARCH=amd64 go build -o ${BINARY_NAME}-prod .
	scp ${BINARY_NAME}-prod $(VPS_USER)@$(VPS_IP):/tmp/${BINARY_NAME}-prod
	ssh $(VPS_USER)@$(VPS_IP) "mv /tmp/${BINARY_NAME}-prod /usr/local/bin/${BINARY_NAME}-prod && chmod +x /usr/local/bin/${BINARY_NAME}-prod && systemctl restart c64proxy-prod"
	rm ${BINARY_NAME}-prod