# 河流 eDNA 样本质量审查工作台

本项目面向河流生态监测团队，以浏览器工作台串联采样批次建档、DNA 提取与测序结果登记、自动质量检查、异常重测、物种鉴定专家复核、物种清单冻结和科研发布凭据签发。凭据可在公开验证页核对快照摘要与验证码，帮助科研发布负责人确认材料没有偏离冻结版本。

服务不依赖外部数据库。所有写操作先经过领域状态机与 `expectedVersion` 乐观并发检查，再写入本地 JSON Lines 事件账本。每条事件包含递增序号、前序摘要和 SHA-256 摘要；服务启动时校验整条链并重放批次投影。当前快照通过同目录临时文件同步后原子替换。

## 构建与测试

项目需要 Go 1.22 或更高版本，且只使用 Go 标准库。

```text
go build ./cmd/edna-workbench
go test ./...
```

## 运行服务

默认监听高位回环地址 `127.0.0.1:19081`，默认数据目录为 `./data`：

```text
go run ./cmd/edna-workbench
```

可以显式指定监听地址和数据目录：

```text
go run ./cmd/edna-workbench -addr=127.0.0.1:19181 -data=./data
```

也可以通过进程环境中的 `PORT` 提供端口号；未显式传入 `-addr` 时，服务会将其绑定为 `127.0.0.1:<PORT>`。服务拒绝 `0.0.0.0`、外部网卡地址、低位端口以及常见开发端口 `3000`、`8080`，避免意外暴露。

启动后在浏览器打开 `http://127.0.0.1:19081/`。页面包含以下可达流程：

1. 在“新建批次”中录入河流、断面、采样日期、采样点和样本条码。
2. 在批次总览登记 DNA 提取批次、测序运行、读数、覆盖度、阴性对照污染率和物种候选证据。
3. 在“质量与复核”执行自动检查；异常结果先登记重测请求，再回到总览录入带 `supersedesResultID` 的替代结果并重新质检。
4. 专家提交物种鉴定复核结论；只有自动质检无失败项且重测全部完成时才能通过。
5. 在“冻结与凭据”生成不可变物种清单和证据摘要，然后签发科研发布凭据。
6. 在“验证凭据”中输入凭据编号，并可同时核对快照摘要与验证码。

## 有界自检

下列命令会在隔离临时数据目录启动真实 HTTP 服务，依次执行“异常原结果→质检失败→重测→替代结果→质检通过→专家通过→冻结→签发→验证”的完整样例流程，检查工作台页面和健康 API，随后主动关闭服务并删除临时数据：

```text
go run ./cmd/edna-workbench -selfcheck -addr=127.0.0.1:19081
```

## 数据与接口

- `data/events.jsonl`：只追加事件账本，包含 `schemaVersion`、序号和摘要链。
- `data/snapshot.json`：可重建的当前投影，使用原子替换写入。
- `Idempotency-Key` 请求头可覆盖 JSON 命令中的 `meta.idempotencyKey`。
- 所有批次写 API 都要求 `meta.expectedVersion`；版本落后时返回 HTTP `409` 和当前版本。
- 主要 API 位于 `/api/batches`、`/api/batches/{batchID}/results`、`/api/batches/{batchID}/quality-check`、`/api/batches/{batchID}/retests`、`/api/batches/{batchID}/expert-review`、`/api/batches/{batchID}/freeze`、`/api/batches/{batchID}/credential` 和 `/api/credentials/verify`。

冻结会阻止后续测序证据修改。物种清单只纳入有效结果中置信度不低于 `0.80` 的候选；凭据验证码由凭据编号、批次编号、快照摘要、物种数、签发人和签发时间确定性生成。
