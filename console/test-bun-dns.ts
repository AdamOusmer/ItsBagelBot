import dns from "node:dns";
dns.setDefaultResultOrder("ipv4first");

const start = performance.now();
try {
  await fetch("https://api.twitch.tv/helix/users");
} catch (e) {
  console.log(e);
}
console.log("Took", performance.now() - start, "ms");
