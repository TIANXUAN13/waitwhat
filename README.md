# WaitWhat

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
- `APP_CORS_ALLOW_ORIGIN`: CORS 允许来源，默认 `*`

## 前端启动

```bash
cd frontend
npm install
npm run dev
```

默认启动地址：`http://localhost:5173`

可选环境变量：

- `VITE_API_BASE`: 前端 API 基地址，默认 `http://localhost:8080/api`

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

## Docker 镜像发布（GitHub Actions）

项目已支持通过 GitHub Actions 自动构建并推送后端镜像到 Docker Hub。

需要在仓库 Secrets 配置：

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

触发方式：

- 推送 tag（例如 `V1234`）自动发布
- 手动触发工作流也可发布

## Docker 镜像运行（单容器前后端）

发布出的镜像已内置前端静态页面与后端 API，启动一个容器即可：

```bash
mkdir -p ./data
sudo chown -R 1000:1000 ./data
docker run -d \
  --name waitwhat \
  --user 1000:1000 \
  -p 8080:8080 \
  -e APP_PORT=8080 \
  -e APP_DATA_DIR=/app/data \
  -e APP_WEB_DIR=/app/web \
  -v "$(pwd)/data:/app/data" \
  2926930231/waitwhat:latest
```

访问：

- 应用主页：`http://127.0.0.1:8080`
- 健康检查：`http://127.0.0.1:8080/api/health`

## Docker Compose 启动

项目已提供 `docker-compose.yaml`，默认同样以 `1000:1000` 用户组运行：

```bash
mkdir -p ./data
sudo chown -R 1000:1000 ./data
docker compose up -d
```

停止：

```bash
docker compose down
```

如果你的服务器启用了 SELinux，请把挂载改成带标签：

```yaml
volumes:
  - ./data:/app/data:Z
```

## 说明

前端当前包含一版高保真原型界面和接口接入结构。  
后端当前提供本地内存演示数据与数据库配置模型；后续可以继续接入真实 SQL 驱动和提醒调度器。
