import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { spawnSync } from "node:child_process";
import process from "node:process";

const root = resolve(import.meta.dirname, "..");
const configPath = resolve(root, "wails.json");
const sourceVersion = readFileSync(resolve(root, "VERSION"), "utf8").trim();
const releaseVersion = process.env.NYAMEDIA_VERSION?.trim() ?? "";
const buildVersion = releaseVersion || `${sourceVersion}-dev`;
const commit = process.env.NYAMEDIA_COMMIT?.trim() || "unknown";
const buildDate = process.env.NYAMEDIA_BUILD_DATE?.trim() || "unknown";
const repository = process.env.NYAMEDIA_UPDATE_REPOSITORY?.trim() || "stevennight/NyaMediaMetadataTool";
const stableVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const repositoryPattern = /^[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+$/;

if (releaseVersion && !stableVersion.test(releaseVersion)) {
  throw new Error(`NYAMEDIA_VERSION must use stable MAJOR.MINOR.PATCH format; received ${JSON.stringify(releaseVersion)}.`);
}
if (!repositoryPattern.test(repository)) {
  throw new Error(`NYAMEDIA_UPDATE_REPOSITORY must be owner/repository; received ${JSON.stringify(repository)}.`);
}
if (!existsSync(configPath)) {
  throw new Error(`Missing Wails configuration: ${configPath}`);
}

const originalConfig = readFileSync(configPath, "utf8");
const config = JSON.parse(originalConfig);
if (releaseVersion) {
  config.info ??= {};
  config.info.productVersion = releaseVersion;
}

const ldflags = [
  `-X main.version=${buildVersion}`,
  `-X main.commit=${commit}`,
  `-X main.buildDate=${buildDate}`,
  `-X main.updateRepository=${repository}`
].join(" ");
const executable = process.platform === "win32" ? "wails.exe" : "wails";
const userArgs = process.argv.slice(2);
const args = ["build", ...userArgs, "-ldflags", ldflags];
const buildStartedAt = Date.now();

try {
  if (releaseVersion) {
    writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`, "utf8");
  }
  const result = spawnSync(executable, args, {
    cwd: root,
    env: process.env,
    stdio: "inherit"
  });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
  if (process.exitCode === 0 && userArgs.includes("-nsis")) {
    const outputDirectory = resolve(root, "build", "bin");
    const installerProduced = existsSync(outputDirectory) && readdirSync(outputDirectory).some((name) => {
      if (!name.toLowerCase().endsWith("-installer.exe")) return false;
      return statSync(resolve(outputDirectory, name)).mtimeMs >= buildStartedAt - 2000;
    });
    if (!installerProduced) {
      throw new Error("Wails completed without producing an NSIS installer. Ensure makensis is installed and available on PATH.");
    }
  }
} finally {
  if (releaseVersion) {
    writeFileSync(configPath, originalConfig, "utf8");
  }
}
