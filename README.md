# Workload HostAlias Operator

一个 Kubernetes Operator，根据 CRD 配置自动管理工作负载（Deployment、DaemonSet、StatefulSet）的 `spec.template.spec.hostAliases`。

## 功能特性

- 跨命名空间管理 Deployment、DaemonSet、StatefulSet 的 `hostAliases`
- 配置变更时自动更新对应工作负载
- 漂移纠正：手动修改管理条目后自动恢复为配置值
- 合并策略：保留工作负载原有 hostAliases，仅添加/更新 CRD 定义的条目
- Finalizer 清理：删除 CRD 时自动移除管理条目和注解
- 冲突检测：防止两个 CRD 管理同一个工作负载
- 高可用：Leader Election + 2 副本部署
- 可观测性：Prometheus 指标、结构化日志、Kubernetes Events

## 快速开始

### 安装 CRD

```bash
make install
```

### 本地运行

```bash
make run
```

### 部署到集群

```bash
# 构建并推送镜像
make docker-build docker-push IMG=<your-registry>/workload-hostalias-operator:latest

# 部署
make deploy IMG=<your-registry>/workload-hostalias-operator:latest
```

## 使用示例

创建一个 `WorkloadHostAlias` 资源，为目标工作负载注入 hostAliases：

```yaml
apiVersion: workload-hostalias-operator/v1alpha1
kind: WorkloadHostAlias
metadata:
  name: example-hostalias
spec:
  target:
    namespace: default
    kind: Deployment
    name: my-app
  hostAliases:
    - ip: "10.0.0.1"
      hostnames:
        - "foo.bar.com"
        - "foo"
    - ip: "10.0.0.2"
      hostnames:
        - "bar.baz.com"
```

### 查看状态

```bash
kubectl get workloadhostaliases
# NAME                TARGET KIND   TARGET NAME   TARGET NAMESPACE   SYNCED   AGE
# example-hostalias   Deployment    my-app        default            True     10s
```

### 支持的工作负载类型

| 类型 | API Group |
|------|-----------|
| Deployment | apps/v1 |
| DaemonSet | apps/v1 |
| StatefulSet | apps/v1 |

## 工作原理

### 三路合并策略

Operator 使用基于注解的三路合并，在注入管理条目的同时保留工作负载原有的 hostAliases：

1. 读取工作负载当前的 `hostAliases`
2. 从 `last-applied-hostaliases` 注解识别上次管理的条目
3. 提取"原始条目"（非 Operator 管理的条目）
4. 合并：期望条目（CRD 定义）+ 原始条目（IP 冲突时期望优先）
5. 写入合并结果并更新注解

效果：
- 工作负载原有的、未在 CRD 中定义的 hostAliases 条目会被保留
- 手动修改了管理条目，下次 Reconcile 会自动纠正回 CRD 定义的值
- 在管理条目之外新增的条目会被保留

### 注解

Operator 会在管理的工作负载上添加三个注解：

| 注解 | 说明 |
|---|---|
| `workload-hostalias-operator/managed` | 值为 `"true"`，标记该工作负载被本 Operator 管理 |
| `workload-hostalias-operator/last-applied-hostaliases` | JSON 格式，记录上次从 CRD 注入的 hostAliases |
| `workload-hostalias-operator/owner` | 指向管理此工作负载的 CRD 实例（`namespace/name`） |

### Finalizer 清理

删除 `WorkloadHostAlias` 时，Finalizer `workload-hostalias-operator/finalizer` 确保：
- 从工作负载中移除 Operator 管理的 hostAliases 条目
- 保留工作负载原有的（非管理的）条目
- 清理所有管理注解

### 冲突检测

如果两个 `WorkloadHostAlias` 资源指向同一个工作负载，Operator 会通过 `owner` 注解检测冲突，拒绝执行更新，并将状态设为 `Synced=False`（原因：`ConflictingOwner`）。

## CRD 字段说明

### Spec

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `spec.target.namespace` | string | 是 | 目标工作负载的命名空间 |
| `spec.target.kind` | string | 是 | 工作负载类型：`Deployment`、`DaemonSet` 或 `StatefulSet` |
| `spec.target.name` | string | 是 | 目标工作负载的名称 |
| `spec.hostAliases[]` | array | 是 | 要注入的 IP-主机名映射列表 |
| `spec.hostAliases[].ip` | string | 是 | IP 地址 |
| `spec.hostAliases[].hostnames[]` | string 数组 | 否 | 该 IP 对应的主机名列表 |

### Status

| 字段 | 类型 | 说明 |
|------|------|------|
| `status.observedGeneration` | int64 | 控制器上次处理的 generation |
| `status.conditions[]` | Condition 数组 | 最新状态（类型：`Synced`） |
| `status.lastSyncTime` | Time | 上次成功同步的时间 |

### Condition 原因

| 原因 | 状态 | 说明 |
|------|------|------|
| `HostAliasesSynced` | True | hostAliases 已成功同步到工作负载 |
| `WorkloadNotFound` | False | 目标工作负载不存在 |
| `ConflictingOwner` | False | 另一个 WorkloadHostAlias 已在管理该工作负载 |
| `UpdateFailed` | False | 更新工作负载失败 |

## 监控指标

通过 `:8080/metrics` 暴露，子系统中文名称为 `workload_host_alias`：

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `workload_host_alias_reconciliation_total` | Counter | `result`（success/error/requeue） | 调和总次数 |
| `workload_host_alias_reconciliation_duration_seconds` | Histogram | - | 调和耗时 |
| `workload_host_alias_managed_workloads` | Gauge | - | 当前管理的工作负载数量 |

## 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--metrics-bind-address` | `:8080` | Metrics 端点监听地址 |
| `--health-probe-bind-address` | `:8081` | 健康检查端点监听地址 |
| `--leader-elect` | `false` | 启用 Leader Election 实现高可用 |

## 开发

### 前置条件

- Go 1.26+
- Docker（或 Podman）
- kubectl + 可访问的 Kubernetes 集群

### 构建与测试

```bash
make build          # 构建二进制
make test           # 运行单元测试和集成测试
make manifests      # 重新生成 CRD 和 RBAC 清单
make generate       # 重新生成 DeepCopy 方法
make run            # 本地运行控制器
```

### 卸载

```bash
make undeploy       # 卸载控制器部署
make uninstall      # 卸载 CRD
```

## 许可证

Unlicense . 随便拿去，爱干啥干啥，都与我无关
