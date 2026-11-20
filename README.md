# agent-env

个人 Go 执行环境项目。默认使用内存模拟后端；v1 支持单集群真实 Pod 冷创建、就绪查询和关闭，见[迭代计划与真集群验收](docs/iteration-plan.md)。Go 客户端位于 `pkg/client`，提供 Create、Get、WaitReady、Close。构建需要 Go 1.22 或更高版本，真实运行另需 kubectl 与测试集群。

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

监听地址可用 `-listen` 修改。模拟记录随进程退出清空。阅读[总体设计](docs/design.md)和[技术选型](docs/technology.md)了解模块和实施顺序。
