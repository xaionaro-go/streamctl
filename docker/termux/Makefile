IMAGE_NAME?=streampanel-android-builder
TAG?=latest

build:
	docker build -t $(IMAGE_NAME) .

publish:
	REGISTRY_IMAGE_NAME=$(IMAGE_NAME) TAG=$(TAG) ./push-to-registry.sh
