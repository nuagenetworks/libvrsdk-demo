all: app

app: 
	go build

deploy: app
	scp demo root@135.121.117.224:/root
