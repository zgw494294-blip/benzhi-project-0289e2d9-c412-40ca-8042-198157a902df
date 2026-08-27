"use strict";

const app = document.querySelector("#app");
const toast = document.querySelector("#toast");
const statuses = {
  draft: "采样建档", results_entered: "结果已登记", quality_failed: "质检异常",
  awaiting_expert: "等待专家复核", approved: "专家已通过", frozen: "清单已冻结", released: "凭据已签发"
};
const stageIndex = {draft: 0, results_entered: 1, quality_failed: 2, awaiting_expert: 3, approved: 4, frozen: 5, released: 5};

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>'"]/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;","'":"&#39;",'"':"&quot;"})[c]);
}
function key(prefix) { return `${prefix}-${Date.now()}-${crypto.getRandomValues(new Uint32Array(1))[0]}`; }
function number(value) { return Number(value); }
function taxaFrom(text) {
  return text.split("\n").filter(Boolean).map(line => {
    const [scientificName, commonName, confidence, readCount, marker] = line.split("|").map(v => v.trim());
    return {scientificName, commonName, confidence: number(confidence), readCount: number(readCount), marker};
  });
}
async function api(path, options = {}) {
  const response = await fetch(path, {headers: {"Content-Type":"application/json", ...(options.headers || {})}, ...options});
  const payload = await response.json().catch(() => ({error:{message:"服务返回了无法解析的响应"}}));
  if (!response.ok) {
    const error = new Error(payload.error?.message || `请求失败 (${response.status})`);
    error.code = payload.error?.code;
    error.currentVersion = payload.error?.currentVersion;
    throw error;
  }
  return payload.data;
}
function flash(message, isError = false) {
  toast.textContent = message; toast.className = isError ? "show error" : "show";
  setTimeout(() => { toast.className = ""; }, 3600);
}
function route(path) { history.pushState({}, "", path); render(); }
document.addEventListener("click", event => {
  const link = event.target.closest("[data-nav]");
  if (!link) return;
  event.preventDefault(); route(new URL(link.href, location.origin).pathname);
});
window.addEventListener("popstate", render);

function bindForm(selector, handler) {
  const form = document.querySelector(selector);
  if (!form) return;
  form.addEventListener("submit", async event => {
    event.preventDefault();
    const button = form.querySelector("button[type=submit]");
    button.disabled = true;
    try { await handler(new FormData(form)); }
    catch (error) { flash(error.message, true); if (error.code === "version_conflict") setTimeout(render, 600); }
    finally { button.disabled = false; }
  });
}
function setActiveNav() {
  document.querySelectorAll("nav a").forEach(a => a.classList.toggle("active", new URL(a.href).pathname === location.pathname));
}
async function render() {
  setActiveNav();
  app.innerHTML = '<section class="loading"><div class="spinner"></div><p>正在读取事件账本投影…</p></section>';
  try {
    const path = location.pathname;
    if (path === "/" ) await renderDashboard();
    else if (path === "/batches/new") renderNewBatch();
    else if (path === "/verify") renderVerify();
    else {
      const match = path.match(/^\/batches\/([^/]+)(?:\/(quality|review|release))?$/);
      if (!match) throw new Error("页面不存在");
      await renderBatch(decodeURIComponent(match[1]), match[2] || "overview");
    }
  } catch (error) {
    app.innerHTML = `<section class="card empty"><h2>无法显示页面</h2><p>${escapeHTML(error.message)}</p><a class="button" href="/" data-nav>返回批次列表</a></section>`;
  }
}

async function renderDashboard() {
  const batches = await api("/api/batches");
  const cards = batches.map(batch => `<article class="card batch-card" data-batch="${escapeHTML(batch.batchID)}">
    <span class="tag ${batch.status}">${statuses[batch.status] || batch.status}</span>
    <h3>${escapeHTML(batch.riverName)}</h3><div class="code">${escapeHTML(batch.batchID)}</div>
    <div class="meta"><span>${escapeHTML(batch.samplingDate)}</span><span>${batch.sampleCount} 个样本</span><span>${batch.resultCount} 条结果</span><span>v${batch.version}</span></div>
    ${batch.failedCount ? `<p class="notice error">${batch.failedCount} 项检查失败，${batch.retestPending} 个重测待处理</p>` : ""}
  </article>`).join("");
  app.innerHTML = `<section class="hero"><h1>从一瓶河水，到可验证的物种清单</h1><p>统一管理采样建档、测序证据、自动质检、异常重测、专家复核和冻结发布。每次业务动作都进入带 SHA-256 摘要链的审计账本。</p></section>
    <div class="toolbar"><div><h2>采样批次</h2><span class="hint">按最近更新排序</span></div><a class="button" href="/batches/new" data-nav>＋ 新建批次</a></div>
    <section class="grid">${cards || '<article class="card empty"><h3>还没有采样批次</h3><p>创建首个批次，登记采样点与样本条码。</p></article>'}</section>`;
  document.querySelectorAll("[data-batch]").forEach(card => card.addEventListener("click", () => route(`/batches/${encodeURIComponent(card.dataset.batch)}`)));
}

function renderNewBatch() {
  app.innerHTML = `<div class="section-title"><div><h1>创建河流采样批次</h1><p>一次提交建立批次、采样点和样本条码，批次从版本 1 开始。</p></div></div>
  <form id="new-batch" class="card">
    <div class="form-grid"><label>批次编号<input name="batchID" required pattern="[A-Za-z0-9][A-Za-z0-9_-]{2,63}" placeholder="YANGTZE-2026-01"></label><label>河流名称<input name="riverName" required placeholder="长江某支流"></label><label>采样日期<input name="samplingDate" type="date" required></label><label>采样员<input name="collector" required></label></div>
    <h3>采样点</h3><div class="form-grid"><label>采样点编号<input name="siteID" required value="SITE-01"></label><label>采样点名称<input name="siteName" required placeholder="上游断面"></label><label>纬度<input name="latitude" type="number" step="0.000001" min="-90" max="90" required></label><label>经度<input name="longitude" type="number" step="0.000001" min="-180" max="180" required></label></div>
    <h3>样本条码</h3><div class="form-grid"><label>样本编号<input name="sampleID" required value="SAMPLE-01"></label><label>条码<input name="barcode" required pattern="[A-Z0-9][A-Z0-9-]{3,31}" placeholder="EDNA-0001"></label><label>样本介质<select name="matrix"><option value="water">水体</option><option value="sediment">沉积物</option><option value="filter">滤膜</option></select></label><label>采集时间<input name="collected" type="datetime-local" required></label></div>
    <button type="submit">创建批次并进入结果登记</button>
  </form>`;
  bindForm("#new-batch", async data => {
    const batchID = data.get("batchID");
    const body = {meta:{expectedVersion:0,idempotencyKey:key("create")}, batchID, riverName:data.get("riverName"), samplingDate:data.get("samplingDate"), collector:data.get("collector"), sites:[{siteID:data.get("siteID"),name:data.get("siteName"),latitude:number(data.get("latitude")),longitude:number(data.get("longitude"))}], samples:[{sampleID:data.get("sampleID"),barcode:data.get("barcode"),siteID:data.get("siteID"),matrix:data.get("matrix"),collectedAt:data.get("collected")} ]};
    await api("/api/batches", {method:"POST",body:JSON.stringify(body)}); flash("采样批次已创建"); route(`/batches/${encodeURIComponent(batchID)}`);
  });
}

function stepBar(status) {
  const labels = ["采样建档","测序登记","自动质检","专家复核","冻结清单","签发凭据"];
  const current = stageIndex[status] ?? 0;
  return `<ol class="steps">${labels.map((label, index) => `<li class="${index < current ? "done" : index === current ? "current" : ""}">${label}</li>`).join("")}</ol>`;
}
async function renderBatch(batchID, view) {
  const response = await api(`/api/batches/${encodeURIComponent(batchID)}`);
  const batch = response.batch;
  app.innerHTML = `<div class="section-title"><div><span class="tag ${batch.status}">${statuses[batch.status] || batch.status}</span><h1>${escapeHTML(batch.riverName)}</h1><p><span class="code">${escapeHTML(batch.batchID)}</span> · ${escapeHTML(response.nextAction)}</p></div><span class="tag">版本 ${batch.version}</span></div>${stepBar(batch.status)}
    <nav class="toolbar"><a class="button secondary" href="/batches/${encodeURIComponent(batchID)}" data-nav>批次总览</a><a class="button secondary" href="/batches/${encodeURIComponent(batchID)}/quality" data-nav>质量与复核</a><a class="button secondary" href="/batches/${encodeURIComponent(batchID)}/release" data-nav>冻结与凭据</a></nav>
    <div id="batch-content"></div>`;
  if (view === "quality" || view === "review") renderQuality(batch);
  else if (view === "release") renderRelease(batch);
  else renderOverview(batch);
}

function renderOverview(batch) {
  const samples = batch.samples.map(sample => `<tr><td>${escapeHTML(sample.sampleID)}</td><td><span class="code">${escapeHTML(sample.barcode)}</span></td><td>${escapeHTML(sample.siteID)}</td><td>${escapeHTML(sample.matrix)}</td><td>${batch.results.filter(r => r.sampleID === sample.sampleID).length}</td></tr>`).join("");
  const results = batch.results.map(result => `<details><summary>${escapeHTML(result.sampleID)} · ${escapeHTML(result.runID)} · <span class="tag ${result.status}">${escapeHTML(result.status)}</span></summary><p>读数 ${result.readCount} · 覆盖度 ${(result.coverage*100).toFixed(1)}% · 阴性对照 ${(result.negativeControlRate*100).toFixed(2)}%</p><div class="digest">证据摘要 ${escapeHTML(result.evidenceDigest)}</div><ul>${result.candidateTaxa.map(t => `<li><i>${escapeHTML(t.scientificName)}</i> ${escapeHTML(t.commonName)} · 置信度 ${(t.confidence*100).toFixed(1)}% · ${escapeHTML(t.marker)}</li>`).join("")}</ul></details>`).join("");
  const mutable = !["frozen","released"].includes(batch.status);
  document.querySelector("#batch-content").innerHTML = `<section class="grid"><article class="card"><span class="hint">样本数</span><span class="metric">${batch.samples.length}</span></article><article class="card"><span class="hint">测序结果</span><span class="metric">${batch.results.length}</span></article><article class="card"><span class="hint">采样点</span><span class="metric">${batch.sites.length}</span></article></section>
  <div class="section-title"><div><h2>样本登记</h2><p>${escapeHTML(batch.samplingDate)} · ${escapeHTML(batch.collector)}</p></div></div><section class="card table-wrap"><table><thead><tr><th>样本</th><th>条码</th><th>采样点</th><th>介质</th><th>结果数</th></tr></thead><tbody>${samples}</tbody></table></section>
  <div class="section-title"><div><h2>测序结果与物种候选</h2><p>原结果和替代结果通过 supersedesResultID 保持关系</p></div></div><section class="card">${results || '<p class="empty">尚未登记测序结果</p>'}</section>
  ${mutable ? resultForm(batch) : '<p class="notice">批次已经冻结，测序证据不可再修改。</p>'}`;
  bindResultForm(batch);
}

function resultForm(batch) {
  const failed = batch.results.filter(r => r.status === "failed");
  return `<div class="section-title"><div><h2>登记测序结果</h2><p>物种证据每行使用“学名 | 中文名 | 置信度 | 候选读数 | 标记”</p></div></div><form id="result-form" class="card"><div class="form-grid"><label>结果编号<input name="resultID" required placeholder="RES-001"></label><label>样本<select name="sampleID">${batch.samples.map(s => `<option>${escapeHTML(s.sampleID)}</option>`).join("")}</select></label><label>DNA 提取批次<input name="extractionLot" required placeholder="EXT-2026-01"></label><label>测序运行编号<input name="runID" required placeholder="RUN-2026-01"></label><label>有效读数<input name="readCount" type="number" min="0" required value="12000"></label><label>覆盖度（0—1）<input name="coverage" type="number" min="0" max="1" step="0.001" required value="0.92"></label><label>阴性对照污染率（0—1）<input name="negativeControlRate" type="number" min="0" max="1" step="0.0001" required value="0.005"></label><label>替代原结果<select name="supersedesResultID"><option value="">非替代结果</option>${failed.map(r => `<option value="${escapeHTML(r.resultID)}">${escapeHTML(r.resultID)} / ${escapeHTML(r.sampleID)}</option>`).join("")}</select></label></div><label>物种候选<textarea name="taxa" required placeholder="Cyprinus carpio | 鲤 | 0.96 | 3200 | 12S"></textarea><span class="hint">一行一个候选，不要省略分隔符。</span></label><button type="submit">保存测序结果</button></form>`;
}
function bindResultForm(batch) {
  bindForm("#result-form", async data => {
    const body = {meta:{expectedVersion:batch.version,idempotencyKey:key("result")},resultID:data.get("resultID"),sampleID:data.get("sampleID"),extractionLot:data.get("extractionLot"),runID:data.get("runID"),readCount:number(data.get("readCount")),coverage:number(data.get("coverage")),negativeControlRate:number(data.get("negativeControlRate")),candidateTaxa:taxaFrom(data.get("taxa")),supersedesResultID:data.get("supersedesResultID")};
    await api(`/api/batches/${encodeURIComponent(batch.batchID)}/results`, {method:"POST",body:JSON.stringify(body)}); flash("测序结果已写入证据账本"); render();
  });
}

function renderQuality(batch) {
  const review = batch.review;
  const checks = review?.checkItems.map(item => `<tr><td>${item.passed ? "✓" : "✕"}</td><td>${escapeHTML(item.label)}</td><td>${escapeHTML(item.sampleID || "批次")}</td><td>${escapeHTML(item.message)}</td><td>${item.observed ?? "—"}</td></tr>`).join("") || "";
  const requests = review?.retestRequests.map(request => `<tr><td>${escapeHTML(request.requestID)}</td><td>${escapeHTML(request.sampleID)}</td><td>${escapeHTML(request.originalResultID)}</td><td>${request.resolvedAt ? `已由 ${escapeHTML(request.replacementResultID)} 替代` : "等待替代结果"}</td></tr>`).join("") || "";
  const canCheck = ["results_entered","quality_failed","awaiting_expert"].includes(batch.status);
  document.querySelector("#batch-content").innerHTML = `${canCheck ? `<form id="quality-form" class="card"><h2>自动质量检查</h2><p>系统检查阴性对照污染率 ≤ 0.02、覆盖度 ≥ 0.80、有效读数 ≥ 1000、样本关联和批次元数据。</p><input type="hidden" name="reviewID" value="QR-${Date.now()}"><button type="submit">执行并记录质量检查</button></form>` : ""}
  ${review ? `<div class="section-title"><div><h2>检查结果</h2><p>${review.failedItems.length ? `${review.failedItems.length} 项未通过` : "全部自动检查通过"}</p></div></div><section class="card table-wrap"><table><thead><tr><th>结论</th><th>检查项</th><th>范围</th><th>说明</th><th>观测值</th></tr></thead><tbody>${checks}</tbody></table></section>` : '<p class="notice">尚未执行自动质量检查。</p>'}
  ${review && batch.status === "quality_failed" ? retestForm(batch) : ""}
  ${requests ? `<div class="section-title"><h2>重测处置记录</h2></div><section class="card table-wrap"><table><thead><tr><th>请求</th><th>样本</th><th>原结果</th><th>进展</th></tr></thead><tbody>${requests}</tbody></table></section>` : ""}
  ${review && ["awaiting_expert","quality_failed"].includes(batch.status) ? expertForm(batch) : ""}
  ${review?.expert ? `<p class="notice">专家 ${escapeHTML(review.expert)}：${escapeHTML(review.decision)}。${escapeHTML(review.remarks)}</p>` : ""}`;
  bindForm("#quality-form", async data => { await api(`/api/batches/${encodeURIComponent(batch.batchID)}/quality-check`, {method:"POST",body:JSON.stringify({meta:{expectedVersion:batch.version,idempotencyKey:key("quality")},reviewID:data.get("reviewID")})}); flash("自动质量检查已完成"); render(); });
  bindForm("#retest-form", async data => { await api(`/api/batches/${encodeURIComponent(batch.batchID)}/retests`, {method:"POST",body:JSON.stringify({meta:{expectedVersion:batch.version,idempotencyKey:key("retest")},requestID:data.get("requestID"),sampleID:data.get("sampleID"),originalResultID:data.get("originalResultID"),reason:data.get("reason"),requestedBy:data.get("requestedBy")})}); flash("重测请求已登记，请到批次总览录入替代结果"); render(); });
  bindForm("#expert-form", async data => { await api(`/api/batches/${encodeURIComponent(batch.batchID)}/expert-review`, {method:"POST",body:JSON.stringify({meta:{expectedVersion:batch.version,idempotencyKey:key("expert")},expert:data.get("expert"),decision:data.get("decision"),remarks:data.get("remarks")})}); flash("专家复核结论已记录"); render(); });
}
function retestForm(batch) {
  const failures = batch.results.filter(r => r.status === "failed");
  return `<div class="section-title"><div><h2>发起异常重测</h2><p>先登记请求，再到批次总览录入替代结果。</p></div></div><form id="retest-form" class="card"><div class="form-grid"><label>请求编号<input name="requestID" required value="RT-${Date.now()}"></label><label>失败结果<select name="originalResultID">${failures.map(r => `<option value="${escapeHTML(r.resultID)}" data-sample="${escapeHTML(r.sampleID)}">${escapeHTML(r.resultID)} / ${escapeHTML(r.sampleID)}</option>`).join("")}</select></label><label>样本编号<input name="sampleID" required value="${escapeHTML(failures[0]?.sampleID || "")}"></label><label>申请人<input name="requestedBy" required></label></div><label>重测原因<textarea name="reason" required></textarea></label><button type="submit">登记重测请求</button></form>`;
}
function expertForm(batch) {
  return `<div class="section-title"><div><h2>物种鉴定专家复核</h2><p>存在失败项或未完成重测时不能提交“通过”。</p></div></div><form id="expert-form" class="card"><div class="form-grid"><label>专家姓名<input name="expert" required></label><label>复核结论<select name="decision"><option value="approved">通过</option><option value="changes_required">需要整改</option><option value="rejected">驳回</option></select></label></div><label>意见或证据说明<textarea name="remarks"></textarea></label><button type="submit">提交专家结论</button></form>`;
}

function renderRelease(batch) {
  const snapshot = batch.snapshot;
  const credential = batch.credential;
  document.querySelector("#batch-content").innerHTML = `${batch.status === "approved" ? `<form id="freeze-form" class="card"><h2>冻结物种清单</h2><p class="notice warn">冻结后样本、测序结果和专家证据都不可修改。系统将聚合置信度不低于 0.80 的有效候选并计算快照摘要。</p><label>发布负责人<input name="frozenBy" required></label><button type="submit">确认冻结不可变快照</button></form>` : ""}
  ${snapshot ? `<section class="card"><span class="tag frozen">不可变快照</span><h2>${escapeHTML(snapshot.riverName)}物种清单</h2><p>${escapeHTML(snapshot.evidenceSummary)}</p><div class="digest">${escapeHTML(snapshot.digest)}</div><div class="table-wrap"><table><thead><tr><th>物种</th><th>中文名</th><th>标记</th><th>样本</th><th>最高置信度</th></tr></thead><tbody>${snapshot.taxa.map(t => `<tr><td><i>${escapeHTML(t.scientificName)}</i></td><td>${escapeHTML(t.commonName)}</td><td>${t.markers.map(escapeHTML).join(", ")}</td><td>${t.sampleIDs.map(escapeHTML).join(", ")}</td><td>${(t.maxConfidence*100).toFixed(1)}%</td></tr>`).join("")}</tbody></table></div></section>` : '<p class="notice">专家复核通过后可冻结清单。</p>'}
  ${batch.status === "frozen" ? `<div class="section-title"><h2>签发科研发布凭据</h2></div><form id="credential-form" class="card"><div class="form-grid"><label>凭据编号<input name="credentialID" required value="EDNA-CRED-${Date.now()}"></label><label>签发人<input name="issuedBy" required></label></div><button type="submit">签发并锁定发布状态</button></form>` : ""}
  ${credential ? `<section class="card"><span class="tag released">有效凭据</span><h2>${escapeHTML(credential.credentialID)}</h2><p>签发人：${escapeHTML(credential.issuedBy)} · 物种数：${credential.taxaCount}</p><p>验证码 <strong class="code">${escapeHTML(credential.verificationCode)}</strong></p><p class="digest">快照摘要 ${escapeHTML(credential.snapshotDigest)}</p><button id="copy-credential" class="secondary">复制验证信息</button></section>` : ""}`;
  bindForm("#freeze-form", async data => { await api(`/api/batches/${encodeURIComponent(batch.batchID)}/freeze`, {method:"POST",body:JSON.stringify({meta:{expectedVersion:batch.version,idempotencyKey:key("freeze")},frozenBy:data.get("frozenBy")})}); flash("物种清单和证据摘要已冻结"); render(); });
  bindForm("#credential-form", async data => { await api(`/api/batches/${encodeURIComponent(batch.batchID)}/credential`, {method:"POST",body:JSON.stringify({meta:{expectedVersion:batch.version,idempotencyKey:key("credential")},credentialID:data.get("credentialID"),issuedBy:data.get("issuedBy")})}); flash("科研发布凭据已签发"); render(); });
  document.querySelector("#copy-credential")?.addEventListener("click", async () => { await navigator.clipboard.writeText(JSON.stringify({credentialID:credential.credentialID,snapshotDigest:credential.snapshotDigest,verificationCode:credential.verificationCode}, null, 2)); flash("验证信息已复制"); });
}

function renderVerify() {
  app.innerHTML = `<section class="hero"><h1>验证科研发布凭据</h1><p>输入凭据编号。还可以提供快照摘要或验证码，用于检查公开材料是否与冻结发布内容完全一致。</p></section><form id="verify-form" class="card"><label>凭据编号<input name="credentialID" required></label><label>快照摘要（可选）<input name="snapshotDigest"></label><label>验证码（可选）<input name="verificationCode"></label><button type="submit">验证完整性</button></form><section id="verification-result"></section>`;
  bindForm("#verify-form", async data => {
    const result = await api("/api/credentials/verify", {method:"POST",body:JSON.stringify({credentialID:data.get("credentialID"),snapshotDigest:data.get("snapshotDigest"),verificationCode:data.get("verificationCode")})});
    document.querySelector("#verification-result").innerHTML = `<div class="section-title"><h2>验证结果</h2></div><article class="card"><span class="tag ${result.valid ? "approved" : "quality_failed"}">${result.valid ? "有效" : "未通过"}</span><h2>${escapeHTML(result.message)}</h2>${result.credential ? `<p>批次 <span class="code">${escapeHTML(result.credential.batchID)}</span> · ${result.credential.taxaCount} 个物种 · ${escapeHTML(result.credential.issuedBy)} 签发</p>` : ""}</article>`;
  });
}

api("/api/health").then(data => { document.querySelector("#health").textContent = `账本序号 ${data.eventSequence} · ${data.batchCount} 个批次`; }).catch(() => { document.querySelector("#health").textContent = "服务状态不可用"; });
render();
