# agent-env 迭代计划

agent-env 为 Agent 提供可申请、可执行、可释放的隔离环境。当前 v1 提供单集群 Pod 生命周期；v2 起的能力均为规划。每次扩展后端或优化启动速度，都复用同一条验收路径：创建环境、等待可用、运行任务、释放资源。

| 版本 | 实现范围 | 验收标准 |
| --- | --- | --- |
| v0：管理接口 | 内存后端保存模拟记录，提供创建、查询和删除接口。 | 无需集群即可测试生命周期；模拟实例明确标记为 simulated。 |
| v1：冷创建 | 由 Kubernetes 调度单个 Linux Pod；Go 客户端按 Pod Ready 条件等待，调用方可取消等待。 | 在测试集群运行下方验收脚本，经 kubectl exec 输出标记，删除后查询得到 404。 |
| v2：执行与文件 | 在沙箱内运行 Go agent，通过短期路由执行 argv、传输工作区文件，用 exec ID 标识每次执行。 | 通过客户端上传工程、运行测试并下载报告；客户端断线与进程非零退出可区分。 |
| v3：故障恢复 | 用 PostgreSQL 保存创建意图、项目配额与租约；控制器核对记录和实际资源。 | 在 Pod 创建后、状态写回前重启服务，重试不产生第二个 Pod；租约过期与用户关闭并发时只释放一次配额。 |
| v4：多集群 | 注册集群能力与容量，选择满足要求的集群，共享 watch 观察就绪状态。 | 在双集群测试中停用一侧，验证选点及恢复；记录 Kubernetes API 请求量、首命令 P95 与回收耗时。 |
| v5：分级预热 | 对比镜像预拉与空闲实例池。先设置固定水位，再根据领取速率和补池耗时调整水位。 | 同一空闲实例不能被重复领取；用相同负载比较冷启动、镜像命中和热池命中的首命令延迟及空闲资源成本。 |
| v6：多运行时 | 为第二种隔离后端实现 provider，按运行时与资源能力匹配请求。 | 复用执行、文件与释放测试；不支持的组合在创建资源前返回明确原因。 |

## v1 的契约与限制

memory 仍是默认后端。显式选择 kube 时使用当前 kubectl context 和指定 namespace；仅操作带管理标签且名称符合生成 ID 的 Pod。创建返回 201 表示 Pod 对象已建立，未承诺可执行。查询返回 pending、ready、closing 或 terminated。关闭的 204 表示 Kubernetes 接受删除；实际回收以查询 404 为准。WaitReady 应传带 deadline 的 context。

v1 刻意没有后台调谐、多租户入口和独立数据面。仅在个人开发集群、独立 namespace 和回环监听下使用。创建中断后可能存在未返回 ID 的对象，按标签盘点清理；Pod 的一小时 activeDeadline 限制运行时长，不会删除已终止对象。单集群实验阶段 kubectl 便于观察与排错，后续替换为 client-go；两者不能同时写同一资源。

## 运行与验收

1. 准备测试集群，确认 `kubectl config current-context`；创建独立命名空间 `kubectl create namespace agent-env`。操作者需具备其中的 Pod 创建、读取、列举、删除以及 smoke 所需的 exec 权限。
2. 在项目根执行 `go run ./cmd/manager -provider kube -namespace agent-env`。默认镜像 busybox:1.36.1，可用 -image 指向集群可拉取的兼容镜像（含 sleep 和 echo，支持非 root）。
3. 在另一终端执行 `bash scripts/smoke-kube.sh`。需要 curl、python3、kubectl。脚本通过管理 API 等待就绪，直接通过 kubectl exec 验证运行环境；失败时尝试删除本次实例。
4. 无集群时执行 `go test -race ./...`。测试覆盖 API 契约、Pod 清单与查询/删除选择条件、就绪及超时；模拟 kubectl 的测试不验证 API Server、镜像拉取和实际调度。
