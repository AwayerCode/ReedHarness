import { spawn } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import { readFile, realpath } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const name = "reed";
export const inject = ["webServer", "skills"];
const origin = "http://127.0.0.1:47831";
const packageRoot = dirname(dirname(fileURLToPath(import.meta.url)));
// Bundled Skills are discovered from the package itself so the plugin never keeps
// a second list of Skill names to maintain.
const skillNames = readdirSync(join(packageRoot, "skills"), { withFileTypes: true }).filter(entry => entry.isDirectory()).map(entry => entry.name).sort();
const routes = ["/reed/", "/reed/health", "/reed/resolve", "/reed/view", "/reed/state"];

export function apply(ctx, config = {}) {
  const skills = skillNames.map(name => {
    const skillRoot = join(packageRoot, "skills", name);
    const entry = readFileSync(join(skillRoot, "SKILL.md"), "utf8");
    const description = entry.match(/^description: (.+)$/m)?.[1];
    if (!description) throw new Error(`${name} Skill description missing`);
    return { name, description, content: entry.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, "") + `\nCLI executable: ${join(packageRoot, "tools/reed/bin", process.platform === "win32" ? "reed.exe" : "reed")}\n`, source: "bundled", path: join(skillRoot, "SKILL.md"), resourceBase: { kind: "directory", path: skillRoot } };
  });
  let starting;
  let child;
  let disposed = false;
  async function connect() {
    const root = await realpath(config.root || process.cwd());
    const runtime = JSON.parse(await readFile(join(packageRoot, "tools/reed/runtime.json"), "utf8"));
    async function health() {
      const response = await fetch(origin + "/reed/health", { signal: AbortSignal.timeout(1500) });
      if (!response.ok) throw new Error("ReedHarness health request failed");
      const info = await response.json();
      if (info.service !== "reed" || info.version !== runtime.version || info.root !== root) {
        const error = new Error("ReedHarness fixed address is occupied by a different version or workspace; stop an independently started service before upgrading");
        error.retiring = info.service === "reed" && info.root === root;
        throw error;
      }
    }
    for (let attempt = 0; ; attempt++) {
      try { await health(); return; } catch (error) {
        if (error.cause?.code === "ECONNREFUSED") break;
        const closing = ["ECONNRESET", "EPIPE", "UND_ERR_SOCKET"].includes(error.cause?.code);
        if ((!error.retiring && !closing) || attempt >= 50 || disposed) throw error;
        await new Promise(resolve => setTimeout(resolve, 100));
      }
    }
    if (disposed) throw new Error("ReedHarness plugin disposed");
    const binary = join(packageRoot, "tools/reed/bin", process.platform === "win32" ? "reed.exe" : "reed");
    child = spawn(binary, ["system-design", "serve", "--root", root, "--expect-version", runtime.version, "--parent-stdin"], { stdio: ["pipe", "pipe", "pipe"] });
    await new Promise((resolve, reject) => {
      let output = "";
      const timer = setTimeout(() => { child.kill(); reject(new Error("ReedHarness startup timed out")); }, 15000);
      child.once("error", error => { clearTimeout(timer); reject(error); });
      child.once("exit", code => { clearTimeout(timer); reject(new Error("ReedHarness exited: " + code)); });
      child.stderr.resume();
      child.stdout.on("data", data => {
        output += data;
        if (output.includes("\n")) {
          clearTimeout(timer);
          try {
            const info = JSON.parse(output.split("\n")[0]);
            if (!info.url) throw new Error("ReedHarness failed to start: " + output);
            resolve();
          } catch (error) { reject(error); }
        }
      });
    });
    await health();
  }
  async function handler(req, res) {
    if (req.method !== "GET" && req.method !== "HEAD") { res.writeHead(405); res.end(); return; }
    try {
      starting ||= connect().catch(error => { starting = undefined; throw error; });
      await starting;
      const requested = new URL(req.url, "http://dsh.invalid");
      const upstream = await fetch(origin + requested.pathname + requested.search, {
        method: req.method, redirect: "manual", signal: AbortSignal.timeout(120000),
        headers: { "Sec-Fetch-Site": req.headers["sec-fetch-site"] || "same-origin" },
      });
      const headers = {};
      for (const key of ["content-type", "content-security-policy", "cache-control", "x-content-type-options", "referrer-policy"]) {
        if (upstream.headers.has(key)) headers[key] = upstream.headers.get(key);
      }
      const body = req.method === "HEAD" ? undefined : Buffer.from(await upstream.arrayBuffer());
      res.writeHead(upstream.status, headers);
      res.end(body);
    } catch (error) {
      starting = undefined;
      res.writeHead(503, { "content-type": "text/plain; charset=utf-8", "cache-control": "no-store" });
      res.end(error.message);
    }
  }
  ctx.effect(() => {
    const cleanup = [];
    try {
      for (const skill of skills) cleanup.push(ctx.skills.register(skill));
      for (const path of routes) cleanup.push(ctx.webServer.register({ kind: "exact", path, handler }));
    } catch (error) { for (const remove of cleanup) remove(); throw error; }
    return () => {
      disposed = true;
      for (const remove of cleanup) remove();
      if (child?.exitCode === null) {
        child.stdin.end();
      }
    };
  }, "reed routes");
}
