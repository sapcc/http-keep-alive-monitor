IMAGE := keppel.eu-de-1.cloud.sap/ccloud/http-keep-alive-monitor
VERSION:= 0.1.6


build:
	docker build -t $(IMAGE):$(VERSION) .

push: build
	docker push $(IMAGE):$(VERSION)
