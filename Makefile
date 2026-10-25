.PHONY: build test vet web-install web-test web-build demo clean

build:
	go build -o bin/pc2twin ./cmd/pc2twin

test:
	go test ./...

vet:
	go vet ./...

web-install:
	cd web && pnpm install

web-test:
	cd web && pnpm test

web-build:
	cd web && pnpm build

# 生成本地演示数据并启动模拟数据源（另开终端执行 make web-dev）
demo:
	go run ./cmd/pc2twin convert -i examples/site.sample.pcd -o out \
		--crs-enu "121.47,31.23,10" --devices examples/devices.example.json
	cd web && node mock/server.mjs --root ../out --port 8787

web-dev:
	cd web && pnpm dev

clean:
	rm -rf bin out out-demo
