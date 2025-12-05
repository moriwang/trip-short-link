# 快速开始指南

> 5 分钟快速部署内部短链接代理服务

## 📋 前置条件

- Docker 和 Docker Compose
- 本地有 `config.json` 配置文件

## 🚀 部署步骤

### 1. 准备配置文件

如果还没有 `config.json`，可以登入后访问 API 导出：

```
http://offlineredirect.ops.ctripcorp.com/api/getAllRecords > config.json
```

### 2. 启动服务

```bash
# 在项目目录下
docker-compose up -d --build
```

### 3. 验证服务

```bash
# 健康检查
curl http://localhost:8712/check | jq

# 测试重定向（假设 idev 存在）
curl -I -H "Host: idev" http://localhost:8712
```

## 🔄 更新配置

修改 `config.json` 后：

```bash
# 热重载（推荐，无停机）
docker-compose exec trip-short-link kill -USR1 1

# 或重启容器
docker-compose restart
```

## 📊 常用命令

```bash
# 查看日志
docker-compose logs -f

# 查看状态
docker-compose ps

# 停止服务
docker-compose down

# 重启服务
docker-compose restart
```

## 🔍 故障排查

### 服务无法启动

```bash
# 检查配置文件
ls -lh config.json

# 查看详细日志
docker-compose logs
```

### 重定向不工作

```bash
# 检查映射是否加载
curl http://localhost:8712/check | jq '.mappings'

# 测试特定域名
curl -v -H "Host: idev" http://localhost:8712
```
---

**就这么简单！** 🎉

