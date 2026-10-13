const http = require('node:http');

const port = Number(process.env.PORT || 3000);
const startedAt = new Date();

const server = http.createServer((request, response) => {
  response.setHeader('Content-Type', 'application/json; charset=utf-8');

  if (request.url === '/health') {
    response.writeHead(200);
    response.end(JSON.stringify({ status: 'ok', uptime: process.uptime() }));
    return;
  }

  response.writeHead(200);
  response.end(JSON.stringify({
    service: 'DeployHub example',
    message: 'Your application is live',
    started_at: startedAt.toISOString()
  }));
});

server.listen(port, '0.0.0.0', () => {
  console.log(`Example service listening on port ${port}`);
});

process.on('SIGTERM', () => {
  server.close(() => process.exit(0));
});
