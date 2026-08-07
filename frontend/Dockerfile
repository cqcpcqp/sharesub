FROM node:22-alpine AS build
RUN corepack enable
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY gateway-cors.conf /etc/nginx/gateway-cors.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
