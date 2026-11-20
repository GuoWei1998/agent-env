# 总体设计方案

版本说明：下文的“首版开发框架”是 v0 基线。v1 已在此基础上实现单集群 Pod 生命周期和 Go client；阶段 2 中的自有 agent 和执行协议留到 v2。以[迭代计划](iteration-plan.md)为当前交付顺序。

目标是让 Agent 申请一个隔离环境，上传工程、执行测试、读取输出和产物，最后释放资源。验收以首次命令成功为准。首版开发框架只打通管理请求，未来执行面不会复用宿主 shell 代替隔离环境。

## 已实现的边界

`cmd/manager` 负责装配和监听，`internal/api` 处理 HTTP 与 JSON，`internal/sandbox` 定义模型、模板校验和 Provider 接口，`internal/provider/memory` 在互斥锁保护下保存模拟实例。入口生成实例 ID；读取返回创建时的记录，删除不存在的实例也成功，便于调用方重复清理。

| HTTP 接口 | 契约 |
| --- | --- |
| GET /healthz | 200 表示管理进程可响应，不代表任何工作负载就绪 |
| POST /v1/sandboxes | 接受 linux 模板，201 + Location；status=simulated 表示只建了本地记录 |
| GET /v1/sandboxes/{id} | 200 返回记录，不存在返回 404 |
| DELETE /v1/sandboxes/{id} | 204，之后查询返回 404 |

## 目标架构与责任

控制面：SDK/CLI → manager → PostgreSQL 保存意图 → controller 选择集群并调用 provider。底层首选 Kubernetes Pod，由集群完成节点调度。sandbox ID 是平台身份，Pod UID 是一次具体实例的身份。运行态以 provider 观测为准。

数据面：SDK 取得短期路由 → worker gateway 验证项目、实例与代次 → Pod 内 agent 提供 argv 执行、带序号的 stdout/stderr 事件和 workspace 文件接口。控制面不转发大文件。每次命令有独立 exec ID；客户端断线、命令取消和沙箱关闭分别处理。

治理：项目配额预留与创建记录在同一事务；使用项目内幂等键防止重复创建。后台续租/到期控制器比对意图和真实对象；删除超时后先对账，确认远端对象消失再释放配额。观测串联 request、sandbox 和 exec ID。

## 分阶段实施与验收

1. **当前：模拟生命周期。** 用 HTTP 契约测试验证创建、读回、重复删除与失败输入。
2. **单集群冷创建。** 增加 kube provider、Go agent 与 SDK；从 kind 创建 Linux Pod，在沙箱中跑测试并下载报告。改用 202 接受异步创建，明确 Pending、Ready、Failed、Closing；Ready 需要 agent 探测成功。
3. **恢复与隔离。** 引入数据库与 controller；模拟 Pod 创建后、状态写回前 manager 退出，验证恢复后只存在一个实例。实现鉴权、配额、短期路由和对跨项目访问的拒绝后再开放共享使用。
4. **预热。** 镜像预拉减少拉取耗时；实例池减少创建与初始化耗时。按模板原子领取空闲实例，关闭即销毁，不回池复用用户环境。对比冷镜像、缓存镜像、热实例的首命令 P95、命中率和空闲 CPU·分钟，再决定是否引入动态水位。
5. **扩展。** 注册第二集群，按资源与运行时能力选点；引入新的隔离后端时复用同一套就绪、命令、文件与回收契约。未确认首次创建结果前不跨集群重试。
