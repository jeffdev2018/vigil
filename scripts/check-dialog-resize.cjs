// Real layout regression: node scripts/check-dialog-resize.cjs
// No API/database/provider. Uses the shared primitives and current web CSS.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { createRequire } = require("node:module");
const { spawn } = require("node:child_process");
const { chromium } = require("@playwright/test");

async function main() {
  const root = path.resolve(__dirname, "..");
  const fixture = await fs.mkdtemp(path.join(root, "packages/views/.dialog-resize-"));
  // PostCSS and its Tailwind plugin are owned by the web app whose CSS we test.
  const webRequire = createRequire(path.join(root, "apps/web/package.json"));
  let server, browser;
  const errors = [];
  try {
    const cssFile = path.join(root, "apps/web/app/globals.css");
    const css = await webRequire("postcss")([webRequire("@tailwindcss/postcss")()])
      .process(await fs.readFile(cssFile, "utf8"), { from: cssFile });
    await fs.writeFile(path.join(fixture, "style.css"), css.css);
    await fs.writeFile(path.join(fixture, "index.html"), '<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/style.css"></head><body><div id="root"></div><script type="module" src="/main.tsx"></script></body></html>');
    await fs.writeFile(path.join(fixture, "vite.config.mjs"), `import react from '@vitejs/plugin-react';export default {plugins:[react()],server:{fs:{allow:[${JSON.stringify(root)}]}},resolve:{dedupe:['react','react-dom']}};`);
    await fs.writeFile(path.join(fixture, "main.tsx"), `
import React from 'react';
import {createRoot} from 'react-dom/client';
import {Dialog,DialogTrigger,DialogContent,DialogTitle} from '../../ui/components/ui/dialog';
import {Popover,PopoverTrigger,PopoverContent} from '../../ui/components/ui/popover';
function App(){return <Dialog><DialogTrigger>Open run</DialogTrigger><DialogContent className="!max-w-5xl !w-[calc(100vw-4rem)] !h-[calc(100vh-4rem)] flex flex-col" showCloseButton={false}>
<DialogTitle>Run details</DialogTitle><div className="flex justify-end"><Popover><PopoverTrigger aria-label="Inspect memory" className="h-7 w-7">i</PopoverTrigger><PopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)]"><details><summary>Memory versions</summary><label>Keep this draft<input aria-label="Memory draft" className="w-full" defaultValue="Original"/></label><div className="max-h-64 overflow-y-auto">{Array.from({length:50},(_,i)=><p key={i}>Version {i+1}</p>)}</div></details></PopoverContent></Popover></div></DialogContent></Dialog>}
createRoot(document.getElementById('root')!).render(<App/>);`);
    server = spawn("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", "0"], { cwd: fixture, detached: true, stdio: ["ignore", "pipe", "pipe"] });
    const url = await new Promise((resolve, reject) => {
      let output = "";
      const timer = setTimeout(() => reject(new Error("Vite startup timeout: " + output)), 20000);
      const read = chunk => {
        output += chunk.toString();
        const match = output.match(/http:\/\/127\.0\.0\.1:\d+\//);
        if (match) { clearTimeout(timer); resolve(match[0]); }
      };
      server.stdout.on("data", read); server.stderr.on("data", read);
      server.once("error", error => { clearTimeout(timer); reject(error); });
      server.once("exit", code => { clearTimeout(timer); reject(new Error("Vite exited " + code + ": " + output)); });
    });
    browser = await chromium.launch({headless:true});
    const page = await browser.newPage({viewport:{width:1200,height:900}});
    page.on("pageerror", error => errors.push(error.message));
    await page.goto(url);
    await page.getByRole("button", {name:"Open run",exact:true}).click();
    await page.getByRole("button", {name:"Inspect memory",exact:true}).click();
    await page.getByText("Memory versions",{exact:true}).click();
    await page.getByRole("textbox", {name:"Memory draft"}).fill("Keep the correction");
    await page.getByText("Memory versions",{exact:true}).click();
    await page.getByText("Memory versions",{exact:true}).click();
    // Finish entrance animation before exercising resize (not mount behavior).
    await page.waitForTimeout(250);
    for (const width of [390,1200,500,390]) {
      await page.setViewportSize({width,height:844});
      await page.waitForTimeout(250);
      const rect = await page.locator('[data-slot="popover-content"]').boundingBox();
      assert(rect && rect.x >= 0 && rect.x + rect.width <= width, `Popover escaped ${width}px: ${JSON.stringify(rect)}`);
      assert.equal(await page.getByRole("textbox",{name:"Memory draft"}).inputValue(),"Keep the correction");
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth),width);
    }
    await page.keyboard.press("Escape");
    await page.locator('[data-slot="popover-content"]').waitFor({state:"hidden"});
    assert.equal(await page.getByRole("button",{name:"Inspect memory",exact:true}).evaluate(e=>e===document.activeElement),true);
    await page.keyboard.press("Escape");
    await page.locator('[data-slot="dialog-content"]').waitFor({state:"hidden"});
    assert.equal(await page.getByRole("button",{name:"Open run",exact:true}).evaluate(e=>e===document.activeElement),true);
    assert.deepEqual(errors,[]);
    console.log("PASS: open dialog/popover resize, retained draft, Escape and focus restoration");
  } finally {
    if (browser) await browser.close();
    if (server?.pid) {
      const exited = new Promise(resolve => server.once("exit",resolve));
      try { process.kill(-server.pid,"SIGTERM"); } catch (error) { if(error.code!=="ESRCH")throw error; }
      await Promise.race([exited,new Promise(resolve=>setTimeout(resolve,1000))]);
    }
    await fs.rm(fixture,{recursive:true,force:true});
  }
}
main().catch(error=>{console.error(error);process.exitCode=1;});
