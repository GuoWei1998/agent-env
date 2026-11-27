# agent-env

个人 Go 执行环境项目。memory 用于管理接口模拟；kube 与本机 Linux Docker 使用同一个工作镜像，通过 Go SDK 执行命令和读写文件。需要 Go 1.25+（workspace 文件操作使用 os.Root）。见[v2 构建与双后端验收](docs/v2-run.md)。

```sh
go test ./...
go run ./cmd/manager
```

在另一终端执行：

```sh
curl -i http://127.0.0.1:8080/healthz
curl -i -H 'Content-Type: application/json' -d '{"template":"linux"}' http://127.0.0.1:8080/v1/sandboxes
# 将下一行中的 ID 替换为创建响应中的 id
curl -i http://127.0.0.1:8080/v1/sandboxes/ID
curl -i -X DELETE http://127.0.0.1:8080/v1/sandboxes/ID
```

监听地址可用 `-listen` 修改。模拟记录随进程退出清空。两种真实后端（本机 Docker / Kubernetes）的启动、镜像准备与示例代码见[使用指南](docs/usage.md)；阅读[总体设计](docs/design.md)和[技术选型](docs/technology.md)了解模块和实施顺序。
