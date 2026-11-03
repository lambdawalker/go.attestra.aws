const {test}=require('node:test');const assert=require('node:assert/strict');const fs=require('node:fs');const vm=require('node:vm');
const html=fs.readFileSync(__dirname+'/web/index.html','utf8'),source=fs.readFileSync(__dirname+'/web/app.js','utf8');
function deferred(){let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve}}
function harness(){let n=0;const revoked=[],requests=[];const elements=Object.fromEntries([...html.matchAll(/id="([^"]+)"/g)].map(m=>[m[1],{value:'',hidden:true,disabled:false,textContent:'',children:[],append(...c){this.children.push(...c)},replaceChildren(){this.children=[]},setAttribute(k,v){this[k]=v},removeAttribute(k){delete this[k]},click(){this.clicked=true},scrollIntoView(){}}]));elements.environment.value='dev';const URLFake=class extends URL{};URLFake.createObjectURL=()=>`blob:${++n}`;URLFake.revokeObjectURL=u=>revoked.push(u);const ctx=vm.createContext({document:{getElementById:id=>elements[id],createElement:()=>({click(){}})},URL:URLFake,URLSearchParams,Blob,TextEncoder,setTimeout,clearTimeout,sessionStorage:{removeItem(){}},location:{search:'',assign(){}},fetch:async(path,options)=>{if(path==='/settings')return {ok:true,json:async()=>({})};const d=deferred();requests.push({path,options,d});return d.promise}});vm.runInContext(source,ctx);vm.runInContext(`token='secret';settings={loginDomain:'https://login.example',clientId:'client',origin:'https://admin.example'}`,ctx);return {elements,requests,revoked,run:s=>vm.runInContext(s,ctx)}}
const response=(body='<html>escaped report</html>',status=200)=>({ok:status===200,status,headers:{get:()=> '2026-11-03T01:00:00Z'},text:async()=>body,json:async()=>status===200?{metadata:{finishedAt:'2026-11-03T01:00:00Z'}}:{error:body}});
test('artifact view is initial, read-only, sandboxed and independent of runtime config',async()=>{assert.match(html,/sandbox="allow-scripts"/);assert.doesNotMatch(html,/allow-same-origin/);const h=harness();const p=h.run('selectEnvironment()');assert.equal(h.requests[0].path,'/api/environments/dev/artifacts-report');assert.equal(h.requests.length,1);assert.equal(h.requests[0].options.headers.authorization,'Bearer secret');h.requests[0].d.resolve(response());await p;assert.equal(h.elements.artifactFrame.src,'blob:1');assert.equal(h.elements.artifactsPanel.hidden,false);assert.equal(h.elements.runtimePanel.hidden,true);assert.match(h.elements.artifactObserved.textContent,/2026|11/);assert.ok(h.requests.every(r=>!r.options.method||r.options.method==='GET'))});
test('environment switch discards stale HTML and clears frame URLs',async()=>{const h=harness();let first=h.run('loadArtifacts()');h.requests[0].d.resolve(response());await first;h.elements.environment.value='qa';const old=h.run('loadArtifacts()');h.elements.environment.value='prod';const latest=h.run('loadArtifacts()');assert.deepEqual(h.revoked,['blob:1']);h.requests[1].d.resolve(response('old'));await old;assert.equal(h.elements.artifactFrame.src,undefined);h.requests[2].d.resolve(response('new'));await latest;assert.equal(h.elements.artifactFrame.src,'blob:2')});
test('logout clears all report content and ignores pending JSON download',async()=>{const h=harness();const load=h.run('loadArtifacts()');h.requests[0].d.resolve(response());await load;const download=h.run('downloadArtifacts()');h.run('endSession()');assert.deepEqual(h.revoked,['blob:1']);assert.equal(h.elements.artifactFrame.src,undefined);h.requests[1].d.resolve(response());await download;assert.equal(h.run('downloadURL'),null);assert.equal(h.elements.console.hidden,true)});
test('auth failure invalidates pending HTML and distinguishes missing/oversize states',async()=>{const h=harness();let p=h.run('loadArtifacts()');h.requests[0].d.resolve(response('missing_snapshot',404));await p;assert.match(h.elements.artifactStatus.textContent,/No deployment snapshot/);p=h.run('loadArtifacts()');h.requests[1].d.resolve(response('report_too_large',413));await p;assert.match(h.elements.artifactStatus.textContent,/Download/);assert.equal(h.elements.artifactDownload.disabled,false);p=h.run('loadArtifacts()');h.requests[2].d.resolve(response('admin_sign_in_required',401));await p;assert.equal(h.run('token'),'');assert.equal(h.elements.artifactFrame.src,undefined);assert.equal(h.elements.console.hidden,true)});
test('revoked artifact access clears existing frame and download URLs',async()=>{const h=harness();const load=h.run('loadArtifacts()');h.requests[0].d.resolve(response());await load;const download=h.run('downloadArtifacts()');h.requests[1].d.resolve(response());await download;assert.equal(h.run('downloadURL'),'blob:2');const denied=h.run('downloadArtifacts()');h.requests[2].d.resolve(response('environment_forbidden',403));await denied;assert.equal(h.elements.artifactFrame.src,undefined);assert.equal(h.run('downloadURL'),null);assert.deepEqual(h.revoked,['blob:1','blob:2'])});
for (const staleKind of ['HTML', 'JSON']) {
  test(`old-environment ${staleKind} 403 preserves an already-rendered current environment`, async () => {
    const h = harness();
    let stale;
    if (staleKind === 'JSON') {
      const initial = h.run('loadArtifacts()');
      h.requests[0].d.resolve(response());
      await initial;
      stale = h.run('downloadArtifacts()');
    } else {
      stale = h.run('loadArtifacts()');
    }
    const staleRequest = h.requests.at(-1);
    h.elements.environment.value = 'prod';
    const latest = h.run('selectEnvironment()');
    h.requests.at(-1).d.resolve(response('prod report'));
    await latest;
    const download = h.run('downloadArtifacts()');
    h.requests.at(-1).d.resolve(response());
    await download;
    h.elements.console.hidden = false;
    const frame = h.elements.artifactFrame.src;
    const downloadURL = h.run('downloadURL');
    const generation = h.run('artifactGeneration');
    const revoked = [...h.revoked];
    staleRequest.d.resolve(response('environment_forbidden', 403));
    await stale;
    assert.equal(h.run('token'), 'secret');
    assert.equal(h.elements.console.hidden, false);
    assert.equal(h.elements.artifactFrame.src, frame);
    assert.equal(h.run('downloadURL'), downloadURL);
    assert.equal(h.run('artifactGeneration'), generation);
    assert.deepEqual(h.revoked, revoked);
  });
}
