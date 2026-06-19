import { connect } from 'nats';

async function main() {
  const nc = await connect({ servers: 'nats://admin:Rz_mGdDSDizFeAvsWdYtCyl4NJ92RNHs@127.0.0.1:4222' });
  const jsm = await nc.jetstreamManager({ domain: 'hub' });
  
  const streams = await jsm.streams.list().next();
  console.log("Streams:", streams.map(s => s.config.name));

  for (const s of streams) {
    const consumers = await jsm.consumers.list(s.config.name).next();
    console.log(`Consumers for ${s.config.name}:`, consumers.map(c => c.name));
  }
  
  const js = nc.jetstream({ domain: 'hub' });
  const kv = await js.views.kv('admin_lanes', { history: 1 });
  const keysIter = await kv.keys();
  const keys = [];
  for await (const k of keysIter) {
    keys.push(k);
  }
  console.log("KV keys:", keys);

  await nc.close();
}
main().catch(console.error);
