# 使用指南

agent-env 给调用方提供**用完即弃的隔离执行环境**：创建沙箱 → 等它就绪 → 上传文件、执行命令、下载产物 → 释放。调用方只需要一个 manager 地址；具体后端（本机 Docker 或 Kubernetes）由部署者决定，客户端协议完全一致。

## 1. 组件与请求流

```
调用方（pkg/client 或任何 HTTP 客户端）
   │  ① POST /v1/sandboxes           创建
   │  ② GET  /v1/sandboxes/{id}      轮询状态，直到 ready
   │  ③ POST /v1/sandboxes/{id}/route 领取短期路由（base_url + token，2 分钟有效）
   ├──────────────────────────► manager（控制面）
   │                              provider=kube  → kubectl → Kubernetes Pod
   │                              provider=docker → Docker Engine API → 本机容器
   │  ④ 带着 Bearer token 直连 worker（数据面，不经过 manager）
   └──────────────────────────► worker agent（镜像内常驻进程，端口 8081）
        PUT/GET /files/{path}      工作区文件传输（16 MiB/文件）
        POST /execs                启动命令，返回 exec ID
        GET /execs/{id}/events     流式读取输出（NDJSON，按 seq 排序，exit 事件收尾）
        DELETE /execs/{id}         取消命令
  ⑤ DELETE /v1/sandboxes/{id}   释放；重复删除安全
```

控制面请求少、要可靠；数据面流量大，直连 worker 不拖慢 manager。token 用 Ed25519 绑定沙箱 ID、实例（Pod UID / 容器 hostname）和有效期，worker 只持有公钥。

## 2. 两种后端

| | docker | kube |
| --- | --- | --- |
| 适用场景 | 本机 Linux + Docker Engine，开发调试 | 任意 Kubernetes 集群 |
| manager 启动 | `-provider docker -image agent-env-worker:v2` | `-provider kube -namespace agent-env -image <镜像>` |
| worker 地址 | 宿主回环地址 + 动态端口（仅同机客户端可用） | Pod IP:8081（客户端必须能访问 Pod 网段） |
| 就绪判定 | Docker healthcheck 探测 `/readyz` | readinessProbe 探测 `/readyz` |
| 生命周期操作 | 直接调 Docker Engine API | 通过 kubectl 子进程操作 Pod |

两种后端运行**同一个 worker 镜像、同一个 agent**，SDK 无需感知差异——只有 route 返回的 `base_url` 不同。

## 3. 准备 worker 镜像

后端只负责把 agent 跑起来；镜像里必须有 `/usr/local/bin/agent`（见仓库根目录 Dockerfile，Alpine 基础镜像 + 静态 Go 二进制，非 root 运行）。

```sh
# 本机 docker 后端：构建到本机 daemon 即可
docker build -t agent-env-worker:v2 .

# kube 后端：推送到集群节点可拉取的仓库，并固定 digest
docker tag agent-env-worker:v2 swr.cn-southwest-2.myhuaweicloud.com/<组织>/agent-env-worker:v2
docker push swr.cn-southwest-2.myhuaweicloud.com/<组织>/agent-env-worker:v2
```

私有仓库需要在命名空间配置拉取凭据（kube 后端的 Pod 清单不带 imagePullSecrets，凭据挂在 ServiceAccount 上）：

```sh
kubectl -n agent-env create secret docker-registry swr-cred \
  --docker-server=swr.cn-southwest-2.myhuaweicloud.com \
  --docker-username='<区域>@<AK>' --docker-password='<docker 登录密码>'
kubectl -n agent-env patch serviceaccount default -p '{"imagePullSecrets":[{"name":"swr-cred"}]}'
```

两个常见网络注意点：

- **镜像源**：集群节点通常无法访问 docker.io；构建基础镜像和推送目标都要选节点可达的仓库（如 SWR）。
- **数据面可达性**：manager 返回的 worker 地址必须从调用方机器可达。docker 后端只给回环地址，客户端须与 manager 同机；kube 后端给 Pod IP，客户端须与集群同 VPC 或有 Pod 网段路由。只转发 manager 端口不满足条件。

## 4. 快速开始

**本机 Docker 后端**

```sh
docker build -t agent-env-worker:v2 .
go run ./cmd/manager -provider docker -image agent-env-worker:v2
# 另一终端
go run ./examples/journey
```

**Kubernetes 后端**

```sh
# 推送镜像并配置拉取凭据（见上节），然后
go run ./cmd/manager -provider kube -namespace agent-env \
  -image swr.cn-southwest-2.myhuaweicloud.com/<组织>/agent-env-worker:v2
# 另一终端（本机须能访问 Pod 网段）
go run ./examples/journey
```

验收脚本 `cmd/smoke` 是更严格的完整旅程：上传测试脚本 → 执行 → 校验输出 → 下载报告比对 → 删除至 404。

## 5. 示例代码

`examples/journey` 演示一次完整旅程：Create → WaitReady → Upload → Exec → Events（流式输出与退出码）→ Download → Close。同一份代码对两种后端通用，改 `-manager` 地址即可切换。

调用方不写 Go 也可以直接使用 HTTP 协议，依次为：

```sh
ID=$(curl -s -H 'Content-Type: application/json' -d '{"template":"linux"}' \
  "$MANAGER/v1/sandboxes" | jq -r .id)                       # ① 创建
curl -s "$MANAGER/v1/sandboxes/$ID"                          # ② 轮询到 ready
ROUTE=$(curl -s -X POST "$MANAGER/v1/sandboxes/$ID/route")   # ③ 取路由
BASE=$(jq -r .base_url <<<"$ROUTE"); TOKEN=$(jq -r .token <<<"$ROUTE")
curl -s -H "Authorization: Bearer $TOKEN" -X PUT --data-binary @main.go \
  "$BASE/files/project/main.go"                              # ④ 上传
EXEC=$(curl -s -H "Authorization: Bearer $TOKEN" -d '{"argv":["go","version"],"cwd":"."}' \
  "$BASE/execs" | jq -r .id)                                 #    执行
curl -sN -H "Authorization: Bearer $TOKEN" "$BASE/execs/$EXEC/events?after=0"  # 读输出
curl -s -X DELETE "$MANAGER/v1/sandboxes/$ID"                # ⑤ 释放
```

## 6. 关键语义与限制

- `201` 只表示沙箱对象已建立；能否使用以状态 `ready` 为准。镜像拉取失败会一直 `pending`，`WaitReady` 必须带超时。
- `DELETE` 返回 `204` 只表示删除被受理；真正回收完成以查询返回 `404` 为准。
- 路由 2 分钟有效，过期重新领取即可；命令不会被重放。命令的终止只由 `DELETE /execs/{id}` 或 `timeout_seconds`（上限 300）触发，客户端断线只停止读取输出。
- 每个 worker 保留最近 32 个执行记录、每次执行最近 128 帧输出；游标过期会得到明确的 410 / `cursor_expired`。
- 单文件传输上限 16 MiB；上传是原子替换，中断不破坏原文件。
- v2 没有鉴权、租约和出网限制，manager 应监听回环或置于可信网络；运行不可信代码需要等 v3 的网络策略与 v6 的更强隔离。
