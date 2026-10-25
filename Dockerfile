# Используем официальный образ Golang для сборки
FROM golang:1.27.3-alpine3.24@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS builder

# Устанавливаем рабочую директорию внутри контейнера
WORKDIR /app

# Копируем файлы go.mod и go.sum для установки зависимостей
COPY go.mod go.sum ./

# Устанавливаем зависимости
RUN go mod download

# Копируем все файлы в рабочую директорию контейнера
COPY . .

# Собираем бинарник Go-приложения
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o JerkyVaultBackend

# Создаем минимальный образ с бинарным файлом
FROM alpine:latest

WORKDIR /root/

# Копируем бинарник из предыдущего шага
COPY --from=builder /app/JerkyVaultBackend .

# Добавляем права на выполнение
RUN chmod +x ./JerkyVaultBackend

# Запускаем приложение
CMD ["./JerkyVaultBackend"]
