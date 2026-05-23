.PHONY: lint format breaking gen clean

lint:
	buf lint

format:
	buf format -w

breaking:
	buf breaking --against '.git#branch=main,subdir=shared/proto'

gen:
	buf generate

clean:
	rm -rf ../libs/go/* ../libs/ts/*
