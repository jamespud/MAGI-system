# Build the React frontend and serve it with nginx.
# Build context is ./frontend (see docker/docker-compose.stack.yml).
FROM node:20-alpine AS builder

WORKDIR /app

COPY package*.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:1.27-alpine

COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80
