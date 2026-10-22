FROM node:24-bookworm-slim AS build
WORKDIR /sdk
# The noncommercial devkit is used for Hugo's personal NAS experiment.
ADD https://github.com/openai/sign-in-with-chatgpt-devkit/archive/0a36fefeb913055c8c7a1a29b63d82b96b2e841a.tar.gz /tmp/sdk.tar.gz
RUN tar -xzf /tmp/sdk.tar.gz --strip-components=1 -C /sdk \
 && npm ci --ignore-scripts --no-audit --no-fund --workspace @siwc/local --include-workspace-root \
 && npm run build --workspace @siwc/local \
 && npm prune --omit=dev --ignore-scripts --no-audit --no-fund --workspace @siwc/local --include-workspace-root
FROM node:24-bookworm-slim
WORKDIR /app
COPY --from=build /sdk/node_modules ./node_modules
COPY --from=build /sdk/packages/local/dist ./sdk
COPY keeper.mjs ./keeper.mjs
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["node", "/app/keeper.mjs", "health"]
ENTRYPOINT ["node", "/app/keeper.mjs"]
CMD ["serve"]
