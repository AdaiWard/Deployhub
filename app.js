const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

const state = {
  backend: false,
  user: null,
  projects: [],
  project: null,
  deployments: [],
  deployment: null,
  eventSource: null,
  metricTimer: null
};

const deployModal = $('#deployModal');
const envModal = $('#envModal');
const projectModal = $('#projectModal');
const searchOverlay = $('#searchOverlay');
const terminal = $('#terminal');
const deployButton = $('#deployButton');
const restartButton = $('#restartButton');
const actionMenu = $('#actionMenu');

function openModal(modal) {
  modal.classList.add('open');
  modal.setAttribute('aria-hidden', 'false');
  document.body.style.overflow = 'hidden';
}

function closeModal(modal) {
  modal.classList.remove('open');
  modal.setAttribute('aria-hidden', 'true');
  document.body.style.overflow = '';
}

function toast(message, type = 'success') {
  const item = document.createElement('div');
  item.className = 'toast';
  const icon = document.createElement('i');
  icon.textContent = type === 'error' ? '!' : '✓';
  if (type === 'error') icon.style.background = 'var(--red)';
  const text = document.createElement('span');
  text.textContent = message;
  item.append(icon, text);
  $('#toastStack').append(item);
  setTimeout(() => item.remove(), 4000);
}

function now() {
  return new Intl.DateTimeFormat('ru', {
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
  }).format(new Date());
}

function shortSha(value = '') {
  return value ? value.slice(0, 7) : 'pending';
}

function timeAgo(value) {
  if (!value) return 'только что';
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
  if (seconds < 60) return `${seconds} сек назад`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} мин назад`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} ч назад`;
  return `${Math.floor(seconds / 86400)} дн назад`;
}

function runtimeName(value) {
  return ({ node: 'Node.js', python: 'Python', java: 'Java', dockerfile: 'Dockerfile', auto: 'Auto runtime' })[value] || value;
}

async function api(path, options = {}) {
  const config = { credentials: 'same-origin', ...options };
  if (config.body && typeof config.body !== 'string') {
    config.headers = { 'Content-Type': 'application/json', ...(config.headers || {}) };
    config.body = JSON.stringify(config.body);
  }
  const response = await fetch(path, config);
  if (response.status === 204) return null;
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

function addLog(label, message, type = 'log-info', status = '', timing = '', timestamp = now()) {
  const line = document.createElement('div');
  line.className = 'log-new';
  const time = document.createElement('time');
  time.textContent = timestamp;
  const badge = document.createElement('span');
  badge.className = type;
  badge.textContent = label;
  const copy = document.createElement('p');
  copy.textContent = message;
  const code = document.createElement('em');
  code.textContent = status;
  const duration = document.createElement('small');
  duration.textContent = timing;
  line.append(time, badge, copy, code, duration);
  terminal.append(line);
  terminal.scrollTop = terminal.scrollHeight;
}

function renderLog(entry) {
  const stamp = new Intl.DateTimeFormat('ru', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(entry.created_at));
  const stream = entry.stream || 'system';
  const label = stream === 'stderr' || stream === 'runtime_error' || stream === 'error' ? 'ERR' : stream === 'system' ? 'INFO' : stream === 'runtime' ? 'APP' : 'BUILD';
  const type = label === 'ERR' ? 'log-post' : label === 'BUILD' ? 'log-build' : label === 'APP' ? 'log-get' : 'log-info';
  addLog(label, entry.message, type, '', '', stamp);
}

function setBusy(button, label) {
  button.dataset.original = button.innerHTML;
  button.classList.add('loading');
  button.innerHTML = `<svg><use href="#i-refresh"/></svg>${label}`;
}

function clearBusy(button) {
  button.classList.remove('loading');
  if (button.dataset.original) button.innerHTML = button.dataset.original;
}

function setServiceStatus(status) {
  const element = $('#serviceStatus');
  const names = {
    queued: 'В очереди', cloning: 'Клонирование', building: 'Сборка', deploying: 'Запуск',
    succeeded: 'Работает', failed: 'Ошибка', cancelled: 'Отменен'
  };
  element.lastChild.textContent = names[status] || 'Не запущен';
  element.style.color = status === 'failed' ? 'var(--red)' : ['queued', 'cloning', 'building', 'deploying'].includes(status) ? 'var(--orange)' : 'var(--teal)';
}

function renderProject(project) {
  state.project = project;
  const [owner, repository] = project.repo_full_name.split('/');
  $('#repoOwner').textContent = owner || 'github';
  $('#repoName').textContent = repository || project.name;
  $('#projectName').textContent = project.name;
  $('#runtimeLabel').textContent = runtimeName(project.runtime);
  $('#currentCommitMeta').textContent = `· ${project.branch}`;
  $('.back-link').innerHTML = `Проекты <span>/</span> ${project.name}`;
  setPublicUrl(`http://${project.subdomain}.localhost`);
  document.title = `${project.name} · DeployHub`;
}

function setPublicUrl(url) {
  if (!url) return;
  const link = $('#publicUrl');
  link.href = url;
  while (link.firstChild) link.removeChild(link.firstChild);
  link.append(document.createTextNode(url.replace(/^https?:\/\//, '') + ' '));
  const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', '#i-external');
  icon.append(use);
  link.append(icon);
  $('#copyUrl').dataset.copy = url;
}

function renderDeployment(deployment) {
  if (!deployment) {
    state.deployment = null;
    setServiceStatus('idle');
    $('#currentCommitMessage').textContent = 'Первый deploy еще не запущен';
    $('#currentCommitSha').textContent = 'pending';
    return;
  }
  state.deployment = deployment;
  setServiceStatus(deployment.status);
  $('#currentCommitMessage').textContent = deployment.commit_message || 'Получение данных commit';
  $('#currentCommitSha').textContent = shortSha(deployment.commit_sha);
  $('#updatedLabel').textContent = `Обновлен ${timeAgo(deployment.finished_at || deployment.created_at)}`;
  if (deployment.url) setPublicUrl(deployment.url);
  if (['queued', 'cloning', 'building', 'deploying'].includes(deployment.status)) {
    if (!deployButton.classList.contains('loading')) setBusy(deployButton, 'Building');
  } else {
    clearBusy(deployButton);
  }
}

function renderHistory(deployments) {
  const list = $('#deployList');
  list.innerHTML = '';
  if (!deployments.length) {
    const empty = document.createElement('div');
    empty.className = 'deploy-row';
    empty.innerHTML = '<div><strong>История пуста</strong><p>Запусти первый deploy для этого проекта</p><small>Ожидание действия</small></div>';
    list.append(empty);
    return;
  }
  deployments.slice(0, 6).forEach((deployment, index) => {
    const row = document.createElement('article');
    row.className = `deploy-row${deployment.status === 'failed' ? ' failed' : ''}${index === 0 ? ' current' : ''}`;
    const dot = document.createElement('span');
    dot.className = `timeline-dot ${deployment.status === 'failed' ? 'error' : 'success'}`;
    dot.textContent = deployment.status === 'failed' ? '!' : '✓';
    const content = document.createElement('div');
    const title = document.createElement('strong');
    title.textContent = shortSha(deployment.commit_sha) + ' ';
    const branch = document.createElement('span');
    branch.textContent = state.project?.branch || 'main';
    title.append(branch);
    const message = document.createElement('p');
    message.textContent = deployment.commit_message || deployment.status;
    const meta = document.createElement('small');
    meta.textContent = `${deployment.status} · ${timeAgo(deployment.created_at)}`;
    content.append(title, message, meta);
    const action = document.createElement('button');
    action.className = deployment.status === 'succeeded' && index > 0 ? 'rollback-button' : 'row-menu';
    action.textContent = deployment.status === 'succeeded' && index > 0 ? 'Rollback' : '•••';
    if (deployment.status === 'succeeded' && index > 0) action.addEventListener('click', () => rollback(deployment.id, action));
    row.append(dot, content, action);
    list.append(row);
  });
}

async function loadProjects() {
  state.projects = await api('/api/projects');
  if (state.projects.length) {
    renderProject(state.projects[0]);
    await loadDeployments();
    await loadMetrics();
    if (state.metricTimer) clearInterval(state.metricTimer);
    state.metricTimer = setInterval(loadMetrics, 5000);
  } else {
    renderDeployment(null);
    toast('Подключи первый GitHub проект');
    openModal(projectModal);
  }
}

async function loadDeployments() {
  if (!state.project) return;
  state.deployments = await api(`/api/projects/${state.project.id}/deployments`);
  renderHistory(state.deployments);
  renderDeployment(state.deployments[0]);
  if (state.deployment) {
    await loadLogs(state.deployment.id);
    if (['queued', 'cloning', 'building', 'deploying'].includes(state.deployment.status)) followDeployment(state.deployment.id);
  }
}

async function loadLogs(deploymentId) {
  const logs = await api(`/api/deployments/${deploymentId}/logs`);
  terminal.innerHTML = '';
  logs.forEach(renderLog);
  terminal.dataset.lastId = logs.length ? logs[logs.length - 1].id : 0;
}

function followDeployment(deploymentId) {
  if (state.eventSource) state.eventSource.close();
  const source = new EventSource(`/api/deployments/${deploymentId}/events`);
  state.eventSource = source;
  source.addEventListener('log', event => {
    const entry = JSON.parse(event.data);
    if (Number(entry.id) > Number(terminal.dataset.lastId || 0)) {
      renderLog(entry);
      terminal.dataset.lastId = entry.id;
    }
  });
  source.addEventListener('status', event => {
    const deployment = JSON.parse(event.data);
    renderDeployment(deployment);
    if (['succeeded', 'failed', 'cancelled'].includes(deployment.status)) {
      source.close();
      loadDeployments().catch(() => {});
      toast(deployment.status === 'succeeded' ? 'Новая версия успешно запущена' : `Deploy завершился: ${deployment.status}`, deployment.status === 'succeeded' ? 'success' : 'error');
    }
  });
}

async function loadMetrics() {
  if (!state.project) return;
  try {
    const metrics = await api(`/api/projects/${state.project.id}/metrics`);
    $('#cpuValue').textContent = `${Number(metrics.cpu_percent || 0).toFixed(1)}%`;
    $('#memoryValue').textContent = `${Math.round(metrics.memory_usage_mb || 0)} MB`;
    const limit = Number(metrics.memory_limit_mb || state.project.memory_mb || 512);
    $('.memory-bar span').style.width = `${Math.min(100, Math.round((metrics.memory_usage_mb || 0) / limit * 100))}%`;
  } catch {}
}

async function bootstrap() {
  try {
    await api('/api/health');
    state.backend = true;
    state.user = await api('/api/me');
    const github = $('#githubConnect');
    $('span:last-child', github).textContent = state.user.github_id ? `@${state.user.login}` : 'Dev mode';
    if (state.user.github_id) github.removeAttribute('href');
    await loadProjects();
    loadRepositoryOptions();
  } catch (error) {
    state.backend = false;
    $('span:last-child', $('#githubConnect')).textContent = 'API offline';
    addLog('INFO', 'Frontend preview mode. Start Docker Compose to enable real deployments.', 'log-info');
    console.info('DeployHub API is not available:', error.message);
  }
}

async function loadRepositoryOptions() {
  if (!state.backend || !state.user?.github_id) return;
  try {
    const repositories = await api('/api/github/repositories');
    const datalist = $('#repositoryOptions');
    datalist.innerHTML = '';
    repositories.forEach(repo => {
      const option = document.createElement('option');
      option.value = repo.full_name;
      option.label = `${repo.language || 'Repository'} · ${repo.private ? 'Private' : 'Public'}`;
      option.dataset.branch = repo.default_branch;
      datalist.append(option);
    });
  } catch {}
}

deployButton.addEventListener('click', () => {
  if (state.backend && !state.project) return openModal(projectModal);
  openModal(deployModal);
});

$$('[data-close-modal]').forEach(button => button.addEventListener('click', () => closeModal(button.closest('.modal-backdrop'))));
$$('.modal-backdrop').forEach(backdrop => backdrop.addEventListener('mousedown', event => {
  if (event.target === backdrop) closeModal(backdrop);
}));

$('#confirmDeploy').addEventListener('click', async () => {
  closeModal(deployModal);
  setBusy(deployButton, 'Queued');
  if (!state.backend || !state.project) {
    addLog('BUILD', 'Preview deployment queued', 'log-build');
    setTimeout(() => { clearBusy(deployButton); toast('Preview deploy completed'); }, 1800);
    return;
  }
  try {
    const deployment = await api(`/api/projects/${state.project.id}/deployments`, { method: 'POST', body: {} });
    state.deployments.unshift(deployment);
    renderHistory(state.deployments);
    renderDeployment(deployment);
    terminal.innerHTML = '';
    followDeployment(deployment.id);
    toast('Deploy добавлен в очередь');
  } catch (error) {
    clearBusy(deployButton);
    toast(error.message, 'error');
  }
});

restartButton.addEventListener('click', async () => {
  if (!state.backend || !state.project) return toast('Сначала запусти Docker Compose', 'error');
  setBusy(restartButton, 'Restarting');
  try {
    await api(`/api/projects/${state.project.id}/restart`, { method: 'POST' });
    toast('Перезапуск добавлен в очередь');
    setTimeout(() => clearBusy(restartButton), 1800);
  } catch (error) {
    clearBusy(restartButton);
    toast(error.message, 'error');
  }
});

$('#newProjectButton').addEventListener('click', () => openModal(projectModal));

$('#createProjectButton').addEventListener('click', async () => {
  const button = $('#createProjectButton');
  const errorBox = $('#projectFormError');
  errorBox.textContent = '';
  if (!state.backend) {
    errorBox.textContent = 'Запусти Docker Compose, чтобы подключить репозиторий';
    return;
  }
  const payload = {
    repo_url: $('#projectRepo').value.trim(),
    name: $('#projectInputName').value.trim(),
    branch: $('#projectBranch').value.trim() || 'main',
    root_directory: $('#projectRoot').value.trim(),
    runtime: $('#projectRuntime').value,
    port: Number($('#projectPort').value),
    healthcheck_path: $('#projectHealth').value.trim() || '/',
    subdomain: $('#projectSubdomain').value.trim(),
    auto_deploy: $('#projectAutoDeploy').checked,
    memory_mb: 512,
    cpu: 0.5
  };
  setBusy(button, 'Connecting');
  try {
    const project = await api('/api/projects', { method: 'POST', body: payload });
    state.projects.unshift(project);
    renderProject(project);
    state.deployments = [];
    renderHistory([]);
    renderDeployment(null);
    closeModal(projectModal);
    toast('Репозиторий подключен');
  } catch (error) {
    errorBox.textContent = error.message;
  } finally {
    clearBusy(button);
  }
});

$('#projectRepo').addEventListener('input', event => {
  const value = event.target.value.trim();
  const name = value.split('/').pop()?.replace(/\.git$/, '') || '';
  if (!$('#projectInputName').value) $('#projectInputName').value = name;
  if (!$('#projectSubdomain').value) $('#projectSubdomain').value = name.toLowerCase().replace(/[^a-z0-9]+/g, '-');
  const option = $$('option', $('#repositoryOptions')).find(item => item.value === value);
  if (option?.dataset.branch) $('#projectBranch').value = option.dataset.branch;
});

$('#moreButton').addEventListener('click', event => {
  event.stopPropagation();
  actionMenu.classList.toggle('open');
});

document.addEventListener('click', event => {
  if (!actionMenu.contains(event.target)) actionMenu.classList.remove('open');
});

$('#envMenuButton').addEventListener('click', async () => {
  actionMenu.classList.remove('open');
  openModal(envModal);
  if (!state.backend || !state.project) return;
  try {
    const values = await api(`/api/projects/${state.project.id}/environment`);
    const table = $('#envTable');
    table.innerHTML = '';
    Object.entries(values).forEach(([key, value]) => addEnvRow(key, value));
    if (!Object.keys(values).length) addEnvRow('', '');
  } catch (error) { toast(error.message, 'error'); }
});

$('#rollbackMenuButton').addEventListener('click', () => {
  actionMenu.classList.remove('open');
  toast('Выбери рабочую версию в истории');
  $('.history-panel').scrollIntoView({ behavior: 'smooth', block: 'center' });
});

async function rollback(deploymentId, button) {
  setBusy(button, 'Queued');
  try {
    const deployment = await api(`/api/deployments/${deploymentId}/rollback`, { method: 'POST' });
    renderDeployment(deployment);
    followDeployment(deployment.id);
    toast('Rollback добавлен в очередь');
  } catch (error) {
    toast(error.message, 'error');
  } finally { clearBusy(button); }
}

$('#copyUrl').addEventListener('click', async event => {
  const value = event.currentTarget.dataset.copy;
  try { await navigator.clipboard.writeText(value); }
  catch {
    const input = document.createElement('textarea');
    input.value = value;
    document.body.append(input);
    input.select();
    document.execCommand('copy');
    input.remove();
  }
  const label = $('span', event.currentTarget);
  label.textContent = 'Готово';
  toast('Адрес скопирован');
  setTimeout(() => label.textContent = 'Копировать', 1500);
});

$('#clearLogs').addEventListener('click', () => {
  terminal.innerHTML = '';
  addLog('INFO', 'Log output cleared', 'log-info');
});

function addEnvRow(key = '', value = '') {
  const row = document.createElement('div');
  row.className = 'env-row';
  const keyInput = document.createElement('input');
  keyInput.value = key;
  keyInput.placeholder = 'VARIABLE_NAME';
  keyInput.setAttribute('aria-label', 'Имя переменной');
  const valueInput = document.createElement('input');
  valueInput.value = value;
  valueInput.placeholder = 'value';
  valueInput.setAttribute('aria-label', 'Значение переменной');
  const remove = document.createElement('button');
  remove.setAttribute('aria-label', 'Удалить');
  remove.innerHTML = '<svg><use href="#i-close"/></svg>';
  remove.addEventListener('click', () => row.remove());
  row.append(keyInput, valueInput, remove);
  $('#envTable').append(row);
  return row;
}

$('#addEnv').addEventListener('click', () => $('input', addEnvRow()).focus());
$$('.env-row button').forEach(button => button.addEventListener('click', () => button.closest('.env-row').remove()));

$('#saveEnv').addEventListener('click', async () => {
  if (!state.backend || !state.project) return closeModal(envModal);
  const values = {};
  $$('.env-row', $('#envTable')).forEach(row => {
    const inputs = $$('input', row);
    if (inputs[0].value.trim()) values[inputs[0].value.trim()] = inputs[1].value;
  });
  try {
    await api(`/api/projects/${state.project.id}/environment`, { method: 'PUT', body: values });
    closeModal(envModal);
    toast('Переменные сохранены. Нужен новый deploy');
  } catch (error) { toast(error.message, 'error'); }
});

function openSearch() {
  searchOverlay.classList.add('open');
  searchOverlay.setAttribute('aria-hidden', 'false');
  document.body.style.overflow = 'hidden';
  setTimeout(() => $('#globalSearch').focus(), 50);
}

function closeSearch() {
  searchOverlay.classList.remove('open');
  searchOverlay.setAttribute('aria-hidden', 'true');
  document.body.style.overflow = '';
  $('#globalSearch').value = '';
  $$('.search-results button').forEach(item => item.hidden = false);
}

$('#searchButton').addEventListener('click', openSearch);
searchOverlay.addEventListener('mousedown', event => { if (event.target === searchOverlay) closeSearch(); });

document.addEventListener('keydown', event => {
  const typing = ['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement.tagName);
  if (event.key === '/' && !typing) { event.preventDefault(); openSearch(); }
  if (event.key === 'Escape') {
    closeSearch();
    $$('.modal-backdrop.open').forEach(closeModal);
    actionMenu.classList.remove('open');
  }
});

$('#globalSearch').addEventListener('input', event => {
  const query = event.target.value.toLocaleLowerCase('ru');
  $$('.search-results button').forEach(item => { item.hidden = !item.textContent.toLocaleLowerCase('ru').includes(query); });
});

$$('.search-results button').forEach(item => item.addEventListener('click', () => {
  closeSearch();
  toast(`Открыто: ${$('strong', item).textContent}`);
}));

$('#menuButton').addEventListener('click', () => $('#sidebar').classList.toggle('open'));
$$('.nav-item').forEach(item => item.addEventListener('click', () => {
  $$('.nav-item').forEach(nav => nav.classList.remove('active'));
  item.classList.add('active');
  if (window.innerWidth <= 760) $('#sidebar').classList.remove('open');
  if (item.dataset.view !== 'project') toast(`Раздел «${$('span:nth-child(2)', item).textContent}» будет следующим экраном`);
}));

$$('[data-view-target]').forEach(button => button.addEventListener('click', () => toast('Открыта история deploy')));

bootstrap();
