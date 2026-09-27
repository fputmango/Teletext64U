BINARY_NAME=petsciiproxy

dev:
	rm -f dist/*
	GOOS=darwin GOARCH=arm64 go build -o ./dist/${BINARY_NAME}-mac-silicon .

# Local-only build with the experimental RAI Televideo station (see rai.go) included, for trying it
# out before deciding whether to support it in the default build that gets committed/deployed.
dev-rai:
	rm -f dist/*
	GOOS=darwin GOARCH=arm64 go build -tags rai -o ./dist/${BINARY_NAME}-mac-silicon .

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

# Upload to Development, with the experimental RAI Televideo station included (see rai.go / dev-rai
# above). Also (re)builds and deploys the CnvPng2Raw converter (source: "01 - cnvPNG2RAW/src/") into
# /var/lib/c64proxy-dev - the same directory teletext64-dev uses below, i.e. where c64proxy-dev
# actually runs from (station folders like RAI-TELEVIDEO/ get created relative to that cwd, and
# rai.go execs "./CnvPng2Raw" relative to it too). This overwrites the same petsciiproxy-dev binary
# release-dev deploys - running plain release-dev afterwards reverts the dev service back to the
# RAI-free build.
release-rai-dev:
	@if [ -z "$(VPS_IP)" ]; then echo "Error: VPS_IP is not set!"; exit 1; fi
	GOOS=linux GOARCH=amd64 go build -tags rai -o ${BINARY_NAME}-dev .
	GOOS=linux GOARCH=amd64 go build -o CnvPng2Raw-dev "01 - cnvPNG2RAW/src/cnvPNG2RAW.go" "01 - cnvPNG2RAW/src/patternTeletextRAI.go"
	scp ${BINARY_NAME}-dev $(VPS_USER)@$(VPS_IP):/tmp/${BINARY_NAME}-dev
	scp CnvPng2Raw-dev $(VPS_USER)@$(VPS_IP):/tmp/CnvPng2Raw-dev
	ssh $(VPS_USER)@$(VPS_IP) "mv /tmp/${BINARY_NAME}-dev /usr/local/bin/${BINARY_NAME}-dev && chmod +x /usr/local/bin/${BINARY_NAME}-dev && mkdir -p /var/lib/c64proxy-dev && mv /tmp/CnvPng2Raw-dev /var/lib/c64proxy-dev/CnvPng2Raw && chmod +x /var/lib/c64proxy-dev/CnvPng2Raw && systemctl restart c64proxy-dev"
	rm ${BINARY_NAME}-dev CnvPng2Raw-dev

# Upload to Production
release-prod:
	@if [ -z "$(VPS_IP)" ]; then echo "Error: VPS_IP is not set!"; exit 1; fi
	GOOS=linux GOARCH=amd64 go build -o ${BINARY_NAME}-prod .
	scp ${BINARY_NAME}-prod $(VPS_USER)@$(VPS_IP):/tmp/${BINARY_NAME}-prod
	ssh $(VPS_USER)@$(VPS_IP) "mv /tmp/${BINARY_NAME}-prod /usr/local/bin/${BINARY_NAME}-prod && chmod +x /usr/local/bin/${BINARY_NAME}-prod && systemctl restart c64proxy-prod"
	rm ${BINARY_NAME}-prod

# Upload TTI files to Development
deploy-tti-dev:
	@if [ -z "$(VPS_IP)" ]; then echo "Error: VPS_IP is not set!"; exit 1; fi
	ssh $(VPS_USER)@$(VPS_IP) "mkdir -p /var/lib/c64proxy-dev/teletext64"
	scp teletext64/*.tti $(VPS_USER)@$(VPS_IP):/var/lib/c64proxy-dev/teletext64/

# Upload TTI files to Production
deploy-tti-prod:
	@if [ -z "$(VPS_IP)" ]; then echo "Error: VPS_IP is not set!"; exit 1; fi
	ssh $(VPS_USER)@$(VPS_IP) "mkdir -p /var/lib/c64proxy-prod/teletext64"
	scp teletext64/*.tti $(VPS_USER)@$(VPS_IP):/var/lib/c64proxy-prod/teletext64/
