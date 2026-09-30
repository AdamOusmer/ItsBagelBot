// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Local HTTP integration harness. Uses the unchanged dashboard guards, session
// codec, routes and RPC client; only the downstream services and login identity
// provider are local fixtures. Never run this entrypoint on a public interface.
// Run from web/dashboard with ISOLATION_NATS_BIN=/path/to/nats-server
// bun run test:account-isolation. No external credentials are needed.
import { Database } from 'bun:sqlite';
import { randomBytes } from 'node:crypto';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { connect } from '@nats-io/transport-node';
import { createServer } from 'vite';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const state = mkdtempSync(resolve(tmpdir(), 'bagel-account-isolation-'));
const origin = 'http://localhost:15173';
const brokerURL = 'nats://127.0.0.1:14222';
const natsExecutable = process.env.ISOLATION_NATS_BIN;
if (!natsExecutable) throw new Error('Set ISOLATION_NATS_BIN to a local nats-server executable');
// Prevent any inherited runtime configuration from reaching real services.
for (const key of Object.keys(process.env)) {
  if (/^(NATS_|VALKEY_|TWITCH_|DEMO$|NODE_NAME$|NEW_RELIC_|SESSION_KEY$)/.test(key)) delete process.env[key];
}
Object.assign(process.env, {
  NEW_RELIC_ENABLED: 'false', DEMO: '0', ORIGIN: origin,
  TWITCH_REDIRECT_URI: origin + '/auth/callback',
  SESSION_KEY: randomBytes(32).toString('base64'),
  NATS_RPC_URL: brokerURL, NATS_URL: brokerURL, NATS_HUB_URL: brokerURL,
  NATS_HOST: '127.0.0.1', NATS_PORT: '14222'
});

const db = new Database(resolve(state, 'users.sqlite'));
db.exec('CREATE TABLE users (id TEXT PRIMARY KEY, login TEXT UNIQUE NOT NULL); CREATE TABLE commands (user_id TEXT NOT NULL REFERENCES users(id), name TEXT NOT NULL, response TEXT NOT NULL, PRIMARY KEY(user_id,name)); CREATE TABLE grants (owner_id TEXT, delegate_id TEXT, sections TEXT);');
const people = [
  { id: '910001', login: 'isolation_alice', marker: 'ALICE_PRIVATE_FIXTURE_7c34' },
  { id: '910002', login: 'isolation_bob', marker: 'BOB_PRIVATE_FIXTURE_9e81' }
];
for (const p of people) {
  db.query('INSERT INTO users VALUES (?,?)').run(p.id,p.login);
  db.query('INSERT INTO commands VALUES (?,?,?)').run(p.id,'isolationmarker',p.marker);
}
const wireCalls: { subject: string; body: Record<string, unknown> }[] = [];
const checks: string[] = [];
const assert = (ok: unknown, label: string) => { if (!ok) throw new Error(label); checks.push(label); };
const commands = (id: string) => db.query('SELECT name,response FROM commands WHERE user_id=?').all(id).map((r: any) => ({...r,aliases:[],perm:'everyone',is_active:true,cooldown:0,stream_online_only:false}));

const broker = Bun.spawn([natsExecutable,'-a','127.0.0.1','-p','14222'], {stdout:'ignore',stderr:'ignore'});
let nc: Awaited<ReturnType<typeof connect>> | undefined;
let vite: Awaited<ReturnType<typeof createServer>> | undefined;
try {
  for (let attempt=0;attempt<40;attempt++) {
    try { nc = await connect({servers:brokerURL,maxReconnectAttempts:0}); break; }
    catch { await Bun.sleep(100); }
  }
  if (!nc) throw new Error('Local NATS did not start');
  const sub=nc.subscribe('bagel.rpc.>');
  const responder=(async()=>{
    for await (const m of sub) {
      const body=JSON.parse(new TextDecoder().decode(m.data));
      wireCalls.push({subject:m.subject,body});
      const id=String(body.user_id ?? body.broadcaster_user_id ?? body.broadcaster_id ?? body.delegate_user_id ?? '');
      const user=db.query('SELECT * FROM users WHERE id=?').get(id) as any;
      let result: any = {};
      if(m.subject.endsWith('.state_get')) result=user?{active:true,status:'paid',onboarded:true,username:user.login,display_name:user.login}:{error:'no such user',code:'not_found'};
      else if(m.subject==='bagel.rpc.broadcaster.status.get') result={active:true,tier:'premium',banned:false};
      else if(m.subject==='bagel.rpc.delegation.access') result={grants:db.query('SELECT owner_id,sections FROM grants WHERE delegate_id=?').all(id).map((g:any)=>({owner_user_id:g.owner_id,owner_login:people.find(p=>p.id===g.owner_id)?.login,sections:JSON.parse(g.sections)}))};
      else if(m.subject.endsWith('.commands.get')) result={commands:commands(id)};
      else if(m.subject==='bagel.rpc.commands.upsert') {
        db.query('INSERT INTO commands VALUES (?,?,?) ON CONFLICT(user_id,name) DO UPDATE SET response=excluded.response').run(id,String(body.name),String(body.response));
        result={ok:true};
      } else if(m.subject.endsWith('.commands.replace')) result={ok:true};
      else if(m.subject.endsWith('.modules.get')||m.subject==='bagel.rpc.modules.list') result={modules:[]};
      else if(m.subject.includes('.fetch')) result={defs:[],keys:[]};
      else if(m.subject.startsWith('bagel.rpc.notifications.')) result={notifications:[],unread_count:0};
      m.respond(new TextEncoder().encode(JSON.stringify(result)));
    }
  })();
  await nc.flush();
  vite=await createServer({root,configFile:resolve(root,'vite.config.ts'),mode:'isolation',envDir:state,server:{host:'127.0.0.1',port:15173,strictPort:true},plugins:[{name:'isolation-fixture-login',configureServer(server){
  server.middlewares.use(async(req,res,next)=>{
    const url=new URL(req.url??'/',origin);
    if(url.pathname!=='/__isolation/login') return next();
    const person=people.find(p=>p.login===url.searchParams.get('as'));
    if(!person){res.statusCode=404;res.end();return;}
    try {
      const {seal,COOKIE,SESSION_TTL_SECONDS}=await server.ssrLoadModule('/src/lib/server/session.ts');
      const now=Math.floor(Date.now()/1000);
      const value=seal({user_id:person.id,login:person.login,display_name:person.login,role:'streamer',sid:randomBytes(16).toString('base64url'),iat:now,expires_at:now+SESSION_TTL_SECONDS});
      res.setHeader('Set-Cookie',`${COOKIE}=${value}; Path=/; HttpOnly; SameSite=Lax`);
      res.statusCode=303;res.setHeader('Location','/commands');res.end();
    } catch(e){res.statusCode=500;res.end(String(e));}
  });
  }}]});
  await vite.listen();
  class Client {
    cookie='';
    async request(path:string,init:RequestInit={}) {
      const headers=new Headers(init.headers);if(this.cookie)headers.set('Cookie',this.cookie);
      const r=await fetch(origin+path,{...init,headers,redirect:'manual'});
      const cookie=r.headers.get('set-cookie');if(cookie)this.cookie=cookie.split(';')[0];
      return {status:r.status,location:r.headers.get('location'),body:await r.text()};
    }
  }
  const clients=[new Client(),new Client()];
  for(let i=0;i<2;i++) {
    const login=await clients[i].request('/__isolation/login?as='+people[i].login);
    assert(login.status===303 && clients[i].cookie.startsWith('bagel_session='),people[i].login+' gets its own signed session');
  }
  assert(clients[0].cookie!==clients[1].cookie,'sessions have different cookies');
  for(let round=0;round<2;round++)for(let i=0;i<2;i++) {
    const own=people[i],other=people[1-i];
    const page=await clients[i].request('/commands');
    assert(page.status===200&&page.body.includes(own.marker)&&!page.body.includes(other.marker),own.login+' sees only its own dashboard (round '+round+')');
    const tampered=await clients[i].request(`/commands?user_id=${other.id}&owner=${other.id}&broadcaster_id=${other.id}&delegate_of=${other.id}`);
    assert(tampered.status===200&&tampered.body.includes(own.marker)&&!tampered.body.includes(other.marker),own.login+' cannot switch board with query parameters');
    const data=await clients[i].request(`/commands/__data.json?user_id=${other.id}`);
    assert(data.status===200&&data.body.includes(own.marker)&&!data.body.includes(other.marker),own.login+' data endpoint remains tenant scoped');
    const entered=await clients[i].request('/delegate/enter?owner='+other.id);
    assert(entered.status===302&&entered.location==='/settings?e=access',own.login+' cannot enter other dashboard without grant');
  }
  for(let i=0;i<2;i++) {
    const own=people[i],other=people[1-i],before=wireCalls.length;
    const form=new URLSearchParams({name:'isolationwrite',response:'WRITE_'+own.id,perm:'everyone',cooldown:'0',is_active:'on',user_id:other.id,broadcaster_user_id:other.id,owner_user_id:other.id});
    const saved=await clients[i].request('/commands?/save',{method:'POST',headers:{Origin:origin,Accept:'application/json','Content-Type':'application/x-www-form-urlencoded'},body:form});
    const writes=wireCalls.slice(before).filter(c=>c.subject==='bagel.rpc.commands.upsert');
    assert(saved.status===200&&writes.length===1&&writes[0].body.user_id===own.id,own.login+' forged write targets authenticated user at RPC boundary');
    assert(!commands(other.id).some(c=>c.name==='isolationwrite'&&c.response==='WRITE_'+own.id),own.login+' cannot mutate other account record');
  }
  const anonymous=new Client();
  const anon=await anonymous.request('/commands');
  assert(anon.status===302&&anon.location?.startsWith('/login'),'anonymous cannot load dashboard');
  const altered=new Client();altered.cookie=clients[0].cookie.slice(0,-4)+'AAAA';
  const invalid=await altered.request('/commands');
  assert(invalid.status===302&&invalid.location?.startsWith('/login'),'tampered authenticated cookie rejected');
  // Positive control: a real grant admits the delegate and scopes the board.
  db.query('INSERT INTO grants VALUES (?,?,?)').run(people[0].id,people[1].id,JSON.stringify(['commands']));
  nc.publish('bagel.cache.invalidate.delegation',new TextEncoder().encode(JSON.stringify({broadcaster_id:people[1].id})));
  await nc.flush();await Bun.sleep(100);
  const grant=await clients[1].request('/delegate/enter?owner='+people[0].id);
  assert(grant.status===302&&grant.location==='/commands','explicit commands grant allows delegated dashboard');
  const delegated=await clients[1].request('/commands');
  assert(delegated.status===200&&delegated.body.includes(people[0].marker)&&!delegated.body.includes(people[1].marker),'authorized delegate sees granted owner data');
  console.log(JSON.stringify({result:'PASS',checks:checks.length,checksPassed:checks,users:people.map(({id,login})=>({id,login})),database:resolve(state,'users.sqlite'),rpcRequests:wireCalls.length,limitations:['Fixture identity login replaces external Twitch OAuth; real signed sessions and app HTTP authorization remain unchanged.','NATS responders and SQLite fixture records replace deployed Go/MySQL services.','No Codex Security plugin used for this harness.']},null,2));
  sub.unsubscribe();await responder;
} finally {
  await vite?.close();await nc?.close();broker.kill();await broker.exited;db.close();
}
// The production invalidation subscriber retries forever after broker shutdown.
// All owned servers and the database are closed above; end this test process.
process.exit(0);
