# WaitWhat Memo

一个支持倒计时提醒、预提醒、多通知渠道的备忘录 Web 应用。

## 技术栈

- 前端：Vue 3 + TypeScript + Vite
- 后端：Go
- 数据库：SQLite / PostgreSQL 二选一初始化

## 当前能力

- 首次启动时可选择 SQLite 或 PostgreSQL
- 自动初始化数据库表结构
- 支持备忘录事件、预提醒点、通知渠道、通知日志的数据模型
- 提供基础 API
- 提供响应式前端原型页面，适配移动端与桌面端

## 目录结构

- `backend`: Go 后端
- `frontend`: Vue 前端

## 后端启动

```bash
cd backend
go run .
```

默认启动地址：`http://localhost:8080`

环境变量：

- `APP_PORT`: 服务端口，默认 `8080`
- `APP_DATA_DIR`: SQLite 数据目录，默认 `./data`

## 前端启动

```bash
cd frontend
npm install
npm run dev
```

默认启动地址：`http://localhost:5173`

## 一键启动

```bash
./dev.sh
```

脚本会同时启动前后端，并在启动后自动检查：

- 后端健康接口：`/api/health`
- 前端首页：`http://localhost:5173`

停止服务：

```bash
./stop.sh
```

如果直接在 `./dev.sh` 运行界面中按 `Ctrl+C`，也会自动清理前后端进程。

## 说明

前端当前包含一版高保真原型界面和接口接入结构。  
后端当前提供本地内存演示数据与数据库配置模型；后续可以继续接入真实 SQL 驱动和提醒调度器。
