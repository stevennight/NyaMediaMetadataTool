import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import process from "node:process";

const root = resolve(import.meta.dirname, "..");
const versionPath = resolve(root, "VERSION");
const wailsPath = resolve(root, "wails.json");
const packagePath = resolve(root, "web", "package.json");
const lockPath = resolve(root, "web", "package-lock.json");
const stableVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

function readJSON(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function writeJSON(path, value) {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

function sourceVersion() {
  return readFileSync(versionPath, "utf8").trim();
}

function assertVersion(version) {
  if (!stableVersion.test(version)) {
    throw new Error(`Version must use stable MAJOR.MINOR.PATCH format; received ${JSON.stringify(version)}.`);
  }
}

function versionLocations() {
  const wails = readJSON(wailsPath);
  const pkg = readJSON(packagePath);
  const lock = readJSON(lockPath);
  return {
    wails: wails.info?.productVersion,
    package: pkg.version,
    lock: lock.version,
    lockRoot: lock.packages?.[""]?.version
  };
}

function check(expected = sourceVersion()) {
  assertVersion(expected);
  const source = sourceVersion();
  const locations = { source, ...versionLocations() };
  const mismatches = Object.entries(locations).filter(([, value]) => value !== expected);
  if (mismatches.length) {
    throw new Error(
      `Version mismatch; expected ${expected}: ${mismatches.map(([name, value]) => `${name}=${JSON.stringify(value)}`).join(", ")}`
    );
  }
  process.stdout.write(`Version ${expected} is consistent.\n`);
}

function setVersion(version) {
  assertVersion(version);
  const wails = readJSON(wailsPath);
  const pkg = readJSON(packagePath);
  const lock = readJSON(lockPath);

  wails.info ??= {};
  wails.info.productVersion = version;
  pkg.version = version;
  lock.version = version;
  lock.packages ??= {};
  lock.packages[""] ??= {};
  lock.packages[""].version = version;

  writeFileSync(versionPath, `${version}\n`, "utf8");
  writeJSON(wailsPath, wails);
  writeJSON(packagePath, pkg);
  writeJSON(lockPath, lock);
  check(version);
}

const [command = "check", value] = process.argv.slice(2);
if (command === "check") {
  check(value || sourceVersion());
} else if (command === "set" && value) {
  setVersion(value);
} else {
  throw new Error("Usage: node scripts/version.mjs check [VERSION] | set VERSION");
}
