# 内核资产不随仓库分发，构建前需要先拉取（约 85MB，缓存在 $TMPDIR/coreassets）
.PHONY: all build core-assets core-verify test test-tui test-cli fmt vet tidy clean release-local

BIN     := flclash-tui
LDFLAGS := -s -w

all: core-assets build

## 拉取并校验内置内核资产（已存在且校验通过则跳过下载）
core-assets:
	python3 internal/coreasset/gen.py

## 只校验，不下载（CI 用）
core-verify:
	python3 internal/coreasset/gen.py --verify

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

## 单元测试（已生成的平台会额外验证释放/校验/自愈）
test:
	go test ./...

## 两套端到端测试
test-tui:
	python3 test/smoke_test.py

test-cli:
	python3 test/cli_test.py

test-all: test build test-cli test-tui

fmt:
	gofmt -w main.go internal

vet:
	go vet ./...

tidy:
	go mod tidy

## 本地交叉编译全部目标平台（产物在 dist/）
release-local: core-assets
	@mkdir -p dist
	@for t in \
		"darwin arm64 " "darwin amd64 " \
		"linux amd64 " "linux arm64 " "linux arm 7" "linux 386 " \
		"windows amd64 " "windows arm64 " ; do \
		set -- $$t; \
		ext=""; [ "$$1" = "windows" ] && ext=".exe"; \
		out="dist/flclash-tui-$$1-$$2$${3:+v$$3}$$ext"; \
		echo "  $$out"; \
		CGO_ENABLED=0 GOOS=$$1 GOARCH=$$2 GOARM=$$3 \
			go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" . || exit 1; \
	done

clean:
	rm -rf dist $(BIN) $(BIN).exe
	python3 internal/coreasset/gen.py --clean
