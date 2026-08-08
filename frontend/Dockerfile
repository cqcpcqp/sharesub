FROM node:22-alpine AS build
ARG SHARESUB_VERSION
ARG SHARESUB_REVISION
RUN corepack enable
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY VERSION /src/VERSION
COPY frontend/ .
RUN test "$SHARESUB_VERSION" = "$(tr -d '\n' < /src/VERSION)" && echo "$SHARESUB_REVISION" | grep -Eq '^[0-9a-f]{40}$'
RUN SHARESUB_BUILD_VERSION="$SHARESUB_VERSION" SHARESUB_BUILD_REVISION="$SHARESUB_REVISION" pnpm build

FROM nginx:1.27-alpine
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY frontend/gateway-cors.conf /etc/nginx/gateway-cors.conf
COPY --from=build /src/frontend/dist /usr/share/nginx/html
EXPOSE 80
