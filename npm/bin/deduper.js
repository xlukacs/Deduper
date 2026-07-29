#!/usr/bin/env node

const { existsSync } = require("node:fs");
const { join } = require("node:path");
const { spawnSync } = require("node:child_process");

const targets = {
  "darwin-arm64": "deduper-darwin-arm64",
  "darwin-x64": "deduper-darwin-x64",
  "linux-arm64": "deduper-linux-arm64",
  "linux-x64": "deduper-linux-x64",
  "win32-x64": "deduper-win32-x64.exe",
};

const target = `${process.platform}-${process.arch}`;
const binaryName = targets[target];
if (!binaryName) {
  console.error(
    `deduper: unsupported platform ${target}; supported platforms: ${Object.keys(targets).join(", ")}`,
  );
  process.exitCode = 1;
} else {
  const binary = join(__dirname, binaryName);
  if (!existsSync(binary)) {
    console.error(`deduper: missing bundled binary for ${target}`);
    process.exitCode = 1;
  } else {
    const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
    if (result.error) {
      console.error(`deduper: could not start bundled binary: ${result.error.message}`);
      process.exitCode = 1;
    } else {
      process.exitCode = result.status === null ? 1 : result.status;
    }
  }
}
