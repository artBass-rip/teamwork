FROM node:22-alpine
WORKDIR /app
COPY package.json ./
COPY src ./src
COPY public ./public
COPY grouping.config.json ./grouping.config.json
RUN mkdir -p /app/data /var/lib/teamwork-llm && chown -R node:node /app /var/lib/teamwork-llm
USER node
ENV PORT=8080 CONFIG_PATH=/app/grouping.config.json MCP_URL=http://jira-mcp:8081/mcp
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD node -e "fetch('http://127.0.0.1:8080/api/health').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"
CMD ["node", "src/server.mjs"]
