# v2 直连执行

默认 memory 不启动 agent，获取 route 返回 501。真实后端需先构建工作镜像；v1 的 sleep-only busybox 不能用于 v2。当前面向同机开发或受信任、可直连的集群网络，manager 只应监听回环。此版本没有多租户鉴权、TLS 终止或共享网关。

## 本机 Docker

在仓库根目录运行（Linux Docker Engine；rootless 环境通过 -docker-socket 指定套接字）：

```sh
docker build -t agent-env-worker:v2 .
go run ./cmd/manager -provider docker -image agent-env-worker:v2
# 另一个终端
go run ./cmd/smoke
```

manager 使用本机 Unix socket 操作容器，镜像须已在 daemon 中（可先 docker pull）。工作容器仅向回环发布动态端口，不挂载 socket。使用完调用 Close；本版 Docker 没有自动租约清理。

## Kubernetes

用同一 Dockerfile 将镜像发布到 worker 可拉取的 registry（跨架构要发布对应平台），将实际摘要设置为 WORKER_IMAGE。为私有仓库配置命名空间的拉取凭据。也可在 kind 中先导入开发镜像。运行：

```sh
go run ./cmd/manager -provider kube -namespace agent-env -image "$WORKER_IMAGE"
# 另一个终端，必须能从这里访问 Pod IP:8081
go run ./cmd/smoke
```

manager 需要专用命名空间的 Pod 创建、查询和删除权限。readinessProbe 探测 agent 的 /readyz。节点能探测 Pod 并不代表外部 SDK 能访问；如果 SDK 网络不可达，数据面失败不会重启命令。该模式不适合没有 Pod 网络路由的远程客户端。

## SDK 及资源边界

依次 Create → WaitReady → Upload → Exec → Events → Download → Close。Exec 返回 ID，Events 接受上一条已消费事件的序号，非零退出是 exit 事件；回调取消或网络断开仅停止读取。Cancel 或 timeout_seconds 才终止前台进程组。每次调用重新申请两分钟路由，不重试命令提交。每个 agent 最多保留 32 个执行记录，完成一分钟后可在新请求时回收；每个执行保留最近 128 帧，游标太旧必须显式处理。上传上限 16 MiB，原子替换。workspace 限制适用于文件 API，不是任意命令的内核级隔离边界。

路由用 Ed25519 绑定沙箱、实例和有效期；容器只收到公钥。manager 默认启动时生成新密钥，所以重启后旧实例需要重建。需要连续运行可通过秘密配置传入 AE_SIGNING_SEED（32 字节随机种子的无填充 URL-safe base64），勿写入镜像、仓库或日志。路由仅在可信网络使用。

运行 go test -race ./... 可在不使用 Docker 或 Kubernetes 的情况下验证本机 HTTP 管理与执行链路。cmd/smoke 是两个真实后端共用的验收程序。
