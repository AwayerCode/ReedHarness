import { mkdir, cp, writeFile, readFile, mkdtemp, rm, rename } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const version = process.argv[2];
if (!/^\d+\.\d+\.\d+(?:-[\w.-]+)?$/.test(version)) throw new Error("plugin version must be SemVer without v prefix");
const dist = join(root, "apps/swe-cli/dist");
const staging = await mkdtemp(join(dist, ".plugin-build-"));
const packages = join(staging, "packages");
const platforms = ["darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64", "windows_amd64", "windows_arm64"];
const optionalDependencies = {};
try {
for (const platform of platforms) {
  const target = join(packages, platform);
  await mkdir(target, { recursive: true });
  await cp(join(root, "plugins/dsh"), target, { recursive: true });
  execFileSync("tar", ["-xzf", join(dist, `swe_${platform}.tar.gz`), "-C", target]);
  const runtime = JSON.parse(await readFile(join(target, "skills/swe/runtime.json"), "utf8"));
  if (runtime.version !== version) throw new Error(`SWE ${platform} runtime version does not match ${version}; rebuild the Skill packages`);
  const manifest = { name: "reed", version, description: "SWE engineering skills and architecture tools", skills: "./skills/" };
  for (const host of ["codex", "claude"]) {
    await mkdir(join(target, `.${host}-plugin`), { recursive: true });
    await writeFile(join(target, `.${host}-plugin/plugin.json`), JSON.stringify(manifest, null, 2) + "\n");
  }
  const pkg = JSON.parse(await readFile(join(target, "package.json"), "utf8"));
  pkg.version = version;
  pkg.os = [platform.split("_")[0] === "windows" ? "win32" : platform.split("_")[0]];
  pkg.cpu = [platform.endsWith("amd64") ? "x64" : "arm64"];
  await writeFile(join(target, "package.json"), JSON.stringify(pkg, null, 2) + "\n");
  execFileSync("tar", ["-czf", join(staging, `reed-plugin_${platform}.tar.gz`), "-C", target, "."]);
  const name = "dsh-reed-" + platform.replace("_", "-");
  optionalDependencies[name] = version;
  const native = join(packages, name);
  await mkdir(native, { recursive: true });
  const binary = platform.startsWith("windows") ? "swe-cli.exe" : "swe-cli";
  await cp(join(target, "skills/swe/bin", binary), join(native, binary));
  await cp(join(target, "skills/swe/runtime.json"), join(native, "runtime.json"));
  await writeFile(join(native, "package.json"), JSON.stringify({ name, version, os: pkg.os, cpu: pkg.cpu, files: [binary, "runtime.json"] }, null, 2) + "\n");
}
const npmRoot = join(packages, "dsh-reed");
await mkdir(npmRoot, { recursive: true });
await cp(join(root, "plugins/dsh"), npmRoot, { recursive: true });
await cp(join(root, "skills/swe"), join(npmRoot, "skills/swe"), { recursive: true });
const npmPackage = JSON.parse(await readFile(join(npmRoot, "package.json"), "utf8"));
npmPackage.version = version;
npmPackage.optionalDependencies = optionalDependencies;
npmPackage.scripts = { postinstall: "node install.mjs" };
await writeFile(join(npmRoot, "package.json"), JSON.stringify(npmPackage, null, 2) + "\n");
await mkdir(join(dist, "plugins"), { recursive: true });
const destination = join(dist, "plugins", version);
const previous = join(staging, "previous");
let hadPrevious = false;
try { await rename(destination, previous); hadPrevious = true; } catch (error) { if (error.code !== "ENOENT") throw error; }
try { await rename(packages, destination); } catch (error) {
  if (hadPrevious) await rename(previous, destination);
  throw error;
}
for (const platform of platforms) {
  const archive = `reed-plugin_${platform}.tar.gz`;
  await rename(join(staging, archive), join(dist, archive));
}
} finally { await rm(staging, { recursive: true, force: true }); }
