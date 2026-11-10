import test from "node:test";
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { createHash } from "node:crypto";
import { readFile, cp, mkdtemp, writeFile, rm, access } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import vm from "node:vm";

test("packaged service, DSH proxy, reload and disposal", async () => {
  const platform = `${process.platform === "win32" ? "windows" : process.platform}_${process.arch === "x64" ? "amd64" : process.arch}`;
  const version = process.env.HARN_TEST_VERSION || "0.1.0";
  const bundle = resolve(`apps/swe-cli/dist/plugins/${version}/${platform}`);
  const root = await mkdtemp(join(tmpdir(), "harn-plugin-test-"));
  let service;
  let proxy;
  try {
    await cp("apps/swe-cli/internal/systemdesign/testdata/project", join(root, "first"), { recursive: true });
    await cp("apps/swe-cli/internal/systemdesign/testdata/project", join(root, "second"), { recursive: true });
    service = spawn(join(bundle, "skills/swe/bin", process.platform === "win32" ? "swe-cli.exe" : "swe-cli"), ["system-design", "serve", "--root", root, "--expect-version", version]);
    await new Promise((done, reject) => {
      const timer = setTimeout(() => reject(new Error("startup timeout")), 10000);
      service.once("error", error => { clearTimeout(timer); reject(error); });
      service.once("exit", code => { clearTimeout(timer); reject(new Error(`startup exited ${code}`)); });
      service.stdout.once("data", data => {
        clearTimeout(timer);
        try { assert.equal(JSON.parse(data).url, "http://127.0.0.1:47831/harn/"); done(); } catch (error) { reject(error); }
      });
    });
    const { apply } = await import(pathToFileURL(join(bundle, "lib/index.js")));
    const routes = new Map();
    let skill;
    let dispose;
    apply({
      effect(callback) { dispose = callback(); },
      skills: { register(value) { skill = value; return () => { skill = undefined; }; } },
      webServer: { register(route) { assert.equal(route.kind, "exact"); routes.set(route.path, route.handler); return () => routes.delete(route.path); } },
    }, { root });
    assert.equal(skill.name, "swe");
    assert.equal(skill.resourceBase.path, join(bundle, "skills/swe"));
    proxy = createServer((request, response) => {
      const handler = routes.get(new URL(request.url, "http://localhost").pathname);
      if (handler) handler(request, response); else { response.writeHead(404); response.end(); }
    });
    await new Promise(done => proxy.listen(0, "127.0.0.1", done));
    const origin = `http://127.0.0.1:${proxy.address().port}`;
    const page = await fetch(origin + "/harn/view?project=first");
    assert.equal(page.status, 200);
    assert.match(page.headers.get("content-security-policy"), /frame-ancestors 'self'/);
    const html = await page.text();
    assert.match(html, /<!doctype html>/i);
    const getState = async project => (await fetch(origin + "/harn/state?project=" + project)).json();
    const before = await getState("first");
    assert.equal(before.valid, true);
    const source = await readFile(join(root, "first/system-design.yaml"), "utf8");
    await writeFile(join(root, "first/system-design.yaml"), "invalid: [");
    assert.equal((await getState("first")).valid, false);
    assert.equal((await getState("second")).valid, true);
    await writeFile(join(root, "first/system-design.yaml"), source);
    assert.equal((await getState("first")).revision, before.revision);
    const script = [...html.matchAll(/<script type="application\/javascript">([\s\S]*?)<\/script>/g)].at(-1)[1];
    let scheduled;
    vm.runInNewContext(script, {
      URL, AbortSignal, setTimeout(callback) { scheduled = callback; },
      location: { href: origin + "/harn/view?project=first" },
      fetch: async address => { assert.equal(String(address), origin + "/harn/state?project=first"); return { ok: true, json: async () => before }; },
      document: { createElement() { throw new Error("unexpected warning"); } },
    });
    await scheduled();
    assert.equal((await fetch(origin + "/harn/view?project=.." )).status, 403);
    dispose();
    assert.equal(routes.size, 0);
    assert.equal(skill, undefined);
    assert.equal((await fetch("http://127.0.0.1:47831/harn/health")).status, 200);
  } finally {
    if (proxy) await new Promise(done => proxy.close(done));
    if (service && service.exitCode === null) {
      const exited = new Promise(done => service.once("exit", done));
      service.kill("SIGTERM");
      await exited;
    }
    await rm(root, { recursive: true, force: true });
  }
});

test("DSH client registers the architecture conversation view", async () => {
  let plugin;
  vm.runInNewContext(await readFile("plugins/dsh/lib/client.js", "utf8"), {
    window: { __ModuleLoader__: { load(module) { plugin = module.factory(() => ({})); } } },
  });
  let registered;
  plugin.apply({ slots: {
    inject(name, callback) { assert.equal(name, "conversation.view"); callback(); },
    register(metadata, component) { registered = metadata; assert.equal(typeof component, "function"); },
  } });
  assert.equal(registered.id, "harn");
});

test("DSH view uses the server-resolved URL for an aliased session directory", async () => {
  let plugin;
  let component;
  let effect;
  const updates = [];
  const cwd = "/workspace-alias/project";
  const React = {
    useState(initial) { return [initial, value => updates.push(value)]; },
    useEffect(callback) { effect = callback; },
    createElement() { return null; },
  };
  vm.runInNewContext(await readFile("plugins/dsh/lib/client.js", "utf8"), {
    AbortController,
    fetch: async address => {
      assert.equal(address, "/harn/resolve?cwd=" + encodeURIComponent(cwd));
      return { ok: true, json: async () => ({ url: "/harn/view?project=real-project" }) };
    },
    window: { __ModuleLoader__: { load(module) { plugin = module.factory(() => React); } } },
  });
  plugin.apply({ slots: { inject(name, callback) { callback(); }, register(metadata, view) { component = view; } } });
  component({ sessionId: "session", useSessions(select) { return select({ byId: { session: { cwd } } }); } });
  const cleanup = effect();
  await new Promise(setImmediate);
  assert.equal(updates.at(-1), "/harn/view?project=real-project");
  cleanup();
});

test("npm platform install binds matching Skill runtime and rejects tampering", async () => {
  const os = process.platform === "win32" ? "windows" : process.platform;
  const arch = process.arch === "x64" ? "amd64" : process.arch;
  const name = `dsh-harn-${os}-${arch}`;
  const version = process.env.HARN_TEST_VERSION || "0.1.0";
  const packages = resolve(`apps/swe-cli/dist/plugins/${version}`);
  const temporary = await mkdtemp(join(tmpdir(), "harn-install-test-"));
  try {
    await cp(join(packages, "dsh-harn"), temporary, { recursive: true });
    await cp(join(packages, name), join(temporary, "node_modules", name), { recursive: true });
    execFileSync(process.execPath, [join(temporary, "install.mjs")]);
    const binary = os === "windows" ? "swe-cli.exe" : "swe-cli";
    assert.deepEqual(await readFile(join(temporary, "skills/swe/bin", binary)), await readFile(join(packages, name, binary)));
    await writeFile(join(temporary, "node_modules", name, binary), "tampered");
    assert.throws(() => execFileSync(process.execPath, [join(temporary, "install.mjs")], { stdio: "pipe" }), /checksum mismatch/);
  } finally { await rm(temporary, { recursive: true, force: true }); }
});

async function waitFor(check) {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise(done => setTimeout(done, 25));
  }
  throw new Error("service lifecycle timed out");
}

test("plugin starts its service and releases the port for an upgraded plugin", async () => {
  const platform = `${process.platform === "win32" ? "windows" : process.platform}_${process.arch === "x64" ? "amd64" : process.arch}`;
  const version = process.env.HARN_TEST_VERSION || "0.1.0";
  const bundle = resolve(`apps/swe-cli/dist/plugins/${version}/${platform}`);
  const temporary = await mkdtemp(join(tmpdir(), "harn-upgrade-test-"));
  const workspace = join(temporary, "workspace");
  const upgraded = join(temporary, "upgraded");
  let dispose;
  let proxy;
  const routes = new Map();
  const closed = async () => {
    try { await fetch("http://127.0.0.1:47831/harn/health"); return false; }
    catch (error) { return error.cause?.code === "ECONNREFUSED"; }
  };
  try {
    await cp("apps/swe-cli/internal/systemdesign/testdata/project", workspace, { recursive: true });
    await cp(bundle, upgraded, { recursive: true });
    const nextVersion = version + "-upgrade-test";
    const executable = process.platform === "win32" ? "swe-cli.exe" : "swe-cli";
    execFileSync("go", ["build", "-ldflags", `-X github.com/AwayerCode/Harn/apps/swe-cli/internal/app.Version=${nextVersion}`, "-o", join(upgraded, "skills/swe/bin", executable), "."], { cwd: resolve("apps/swe-cli") });
    const runtime = JSON.parse(await readFile(join(upgraded, "skills/swe/runtime.json"), "utf8"));
    runtime.version = nextVersion;
    runtime.assets[platform.replace("_", "/")].sha256 = createHash("sha256").update(await readFile(join(upgraded, "skills/swe/bin", executable))).digest("hex");
    await writeFile(join(upgraded, "skills/swe/runtime.json"), JSON.stringify(runtime));
    const mount = async directory => {
      const { apply } = await import(pathToFileURL(join(directory, "lib/index.js")));
      apply({ effect(callback) { dispose = callback(); }, skills: { register() { return () => {}; } }, webServer: {
        register(route) { routes.set(route.path, route.handler); return () => routes.delete(route.path); },
      } }, { root: workspace });
    };
    proxy = createServer((request, response) => {
      const handler = routes.get(new URL(request.url, "http://localhost").pathname);
      if (handler) handler(request, response); else { response.writeHead(404); response.end(); }
    });
    await new Promise(done => proxy.listen(0, "127.0.0.1", done));
    const origin = `http://127.0.0.1:${proxy.address().port}`;
    await mount(bundle);
    assert.equal((await (await fetch(origin + "/harn/health")).json()).version, version);
    dispose();
    await mount(upgraded);
    const response = await fetch(origin + "/harn/health");
    assert.equal(response.status, 200, await response.clone().text());
    assert.equal((await response.json()).version, nextVersion);
    dispose();
    dispose = undefined;
    await waitFor(closed);
  } finally {
    dispose?.();
    if (proxy) await new Promise(done => proxy.close(done));
    await waitFor(closed);
    await rm(temporary, { recursive: true, force: true });
  }
});

test("rebuilding a version removes stale files from packages and archives", async () => {
  const version = process.env.HARN_TEST_VERSION || "0.1.0";
  const root = resolve(`apps/swe-cli/dist/plugins/${version}`);
  const stale = ["darwin_arm64/skills/swe/removed.md", "dsh-harn/skills/swe/removed.md", "dsh-harn-darwin-arm64/removed.txt"];
  for (const file of stale) await writeFile(join(root, file), "removed source");
  execFileSync(process.execPath, ["plugins/package.mjs", version]);
  for (const file of stale) await assert.rejects(access(join(root, file)), { code: "ENOENT" });
  const files = execFileSync("tar", ["-tzf", "apps/swe-cli/dist/harn-plugin_darwin_arm64.tar.gz"], { encoding: "utf8" });
  assert.doesNotMatch(files, /removed\.md/);
});
