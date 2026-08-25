# avatars

三个 Agent 的头像源图。提交进仓是因为它们丢过两次——不是文件被删，是**服务端在错的目录找它们**。

## 丢失的真实原因

`LOCAL_UPLOAD_DIR` 在 `.env` 里默认写成相对路径 `./data/uploads`，它按**服务进程的工作目录**解析：

- `make start` 从仓根起服务 → 上传落在 `<仓根>/data/uploads`
- 手工 `cd server && go run ./cmd/server` 起 → 服务去 `server/data/uploads` 找

于是数据库里的 `avatar_url` 一直是好的，文件也一直在，只是两边对不上，页面上头像就变成默认字母块。**在 `.env` 里把 `LOCAL_UPLOAD_DIR` 写成绝对路径**，两种起法都不受影响。

`data/` 是运行时上传目录，被 `.gitignore` 挡着（仓里不该存运行时二进制），所以真正需要提交的是这里的源图。

## 重新上传

```bash
multica --profile local agent list --output json | jq -r '.[] | "\(.name) \(.id)"'   # 取 UUID
multica --profile local agent avatar <agent-uuid> --file avatars/claude.png
```

`agent avatar` **只认 UUID，不认名字**。

| 文件 | 对应 Agent |
|---|---|
| `claude.png` | Claude |
| `ChatGPT.png` | Codex |
| `GLM.jpeg` | GLM |
