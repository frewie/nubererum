const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
  testDir: "./tests",
  testMatch: "**/*.spec.js",
  reporter: "line",
  use: {
    baseURL: "http://127.0.0.1:4180",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "go run ./cmd/nubererum",
    env: {
      ...process.env,
      HOST: "127.0.0.1",
      PORT: "4180",
      APP_PASSWORD: "",
      THINGS_USERNAME: "",
      THINGS_EMAIL: "",
      THINGS_PASSWORD: "",
      THINGS_TOKEN: "",
      THINGS_CLOUD_PASSWORD: "",
    },
    url: "http://127.0.0.1:4180/api/status",
    reuseExistingServer: false,
    timeout: 120000,
  },
});
