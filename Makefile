IMAGE := keppel.eu-de-1.cloud.sap/ccloud/http-keep-alive-monitor
VERSION:= 0.4.7


build:
	podman build -t $(IMAGE):$(VERSION) .

push: build
	podman push $(IMAGE):$(VERSION)
