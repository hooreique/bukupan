const { test, expect } = require('./fixtures.cjs');

async function openDrawing(page, url) {
  await page.goto(url);
  await expect(page.locator('#status')).toHaveText('저장됨');
}
async function saved(page) { await expect(page.locator('#status')).toHaveText('저장됨'); }
async function stroke(page, a, b) {
  await page.mouse.move(...a); await page.mouse.down();
  await page.mouse.move(...b, {steps:12}); await page.mouse.up(); await saved(page);
}
async function pixels(page, points) {
  return page.evaluate(points => new Promise(resolve => {
    // Render and read in the same animation frame, before the default framebuffer
    // is discarded. This exercises the production renderer without test hooks.
    window.dispatchEvent(new Event('resize'));
    requestAnimationFrame(() => {
      const canvas=document.querySelector('canvas'), gl=canvas.getContext('webgl2');
      resolve(points.map(([x,y]) => {
        const p=new Uint8Array(4);
        gl.readPixels(Math.floor(x*canvas.width/innerWidth),canvas.height-1-Math.floor(y*canvas.height/innerHeight),1,1,gl.RGBA,gl.UNSIGNED_BYTE,p);
        return p[0];
      }));
    });
  }), points);
}
async function pendingCount(page) {
  return page.evaluate(() => new Promise((resolve,reject) => {
    const request=indexedDB.open('bukupan',1);
    request.onsuccess=()=>{const db=request.result;const read=db.transaction('pending').objectStore('pending').count();read.onsuccess=()=>{resolve(read.result);db.close();};read.onerror=()=>reject(read.error);};
    request.onerror=()=>reject(request.error);
  }));
}

test('WebGL2 pixels: world precision, joins, zoom, partial erasure and bounded capacity', async ({ page, appURL }) => {
  await page.goto(`${appURL}/__tests/scripts/test-renderer.html`);
  await expect(page.locator('#result')).toHaveText(/^PASS/);
});

test('real pointer sharing, partial erase, own undo and process restart preserve the document', async ({ page, otherPage, app }) => {
  await Promise.all([openDrawing(page,app.url),openDrawing(otherPage,app.url)]);
  await stroke(page,[160,240],[480,240]);
  await expect.poll(()=>pixels(otherPage,[[200,240],[320,240],[440,240]])).toEqual([255,255,255]);
  await saved(page);
  await page.getByRole('button',{name:'지우개',exact:true}).click();
  await stroke(page,[320,210],[320,270]);
  await expect.poll(()=>pixels(otherPage,[[200,240],[320,240],[440,240]])).toEqual([255,0,255]);
  // A later contribution must survive the other participant's undo.
  await saved(otherPage); await stroke(otherPage,[600,180],[600,300]);
  await saved(page); await page.getByRole('button',{name:'취소',exact:true}).click(); await saved(page);
  await expect.poll(()=>pixels(page,[[320,240],[600,240]])).toEqual([255,255]);
  await saved(page); await page.getByRole('button',{name:'취소',exact:true}).click(); await saved(page);
  await expect.poll(()=>pixels(otherPage,[[320,240],[600,240]])).toEqual([0,255]);
  await app.crashAndRestart();
  await Promise.all([saved(page),saved(otherPage)]);
  await openDrawing(page,app.url);
  await expect.poll(()=>pixels(page,[[320,240],[600,240]])).toEqual([0,255]);
});

test('camera is independent, zoom is anchored, pan and home return to the same world', async ({ page, otherPage, appURL }) => {
  await Promise.all([openDrawing(page,appURL),openDrawing(otherPage,appURL)]);
  await stroke(page,[160,240],[480,240]);
  await page.mouse.move(320,240); await page.mouse.wheel(0,-300);
  await expect(page.locator('#zoom-level')).not.toHaveText('100%'); await saved(page);
  await expect.poll(()=>pixels(page,[[320,240]])).toEqual([255]);
  await expect(otherPage.locator('#zoom-level')).toHaveText('100%');
  await saved(page); await page.getByRole('button',{name:'이동',exact:true}).click();
  await page.mouse.move(320,240);await page.mouse.down();await page.mouse.move(320,400,{steps:5});await page.mouse.up();await saved(page);
  await expect.poll(()=>pixels(page,[[320,400],[320,240]])).toEqual([255,0]);
  await page.getByRole('button',{name:'처음 위치',exact:true}).click();await saved(page);
  await expect(page.locator('#zoom-level')).toHaveText('100%');
  await expect.poll(()=>pixels(page,[[320,240]])).toEqual([255]);
});

test('lost acknowledgement survives reload and retries exactly once', async ({ page, appURL }) => {
  await page.addInitScript(() => {
    const Native=WebSocket;
    window.WebSocket=class extends Native {
      constructor(...args) {
        super(...args);
        this.addEventListener('message',event=>{
          if (!(event.data instanceof ArrayBuffer)) return;
          const message=JSON.parse(new TextDecoder().decode(event.data));
          if(message.type==='ack'&&!sessionStorage.getItem('allow-ack')){
            event.stopImmediatePropagation();window.receiptLost=true;this.close();
          }
        });
      }
    };
  });
  await openDrawing(page,appURL);
  await page.mouse.click(300,240);
  await expect.poll(()=>page.evaluate(()=>window.receiptLost===true)).toBe(true);
  await expect.poll(()=>pendingCount(page)).toBe(1);
  await page.evaluate(()=>sessionStorage.setItem('allow-ack','1'));
  await openDrawing(page,appURL);
  await expect.poll(()=>pendingCount(page)).toBe(0);
  await expect.poll(()=>pixels(page,[[300,240]])).toEqual([255]);
  await saved(page);await page.getByRole('button',{name:'취소',exact:true}).click();await saved(page);
  await expect.poll(()=>pixels(page,[[300,240]])).toEqual([0]);
});

test('two-finger touch navigation leaves no accidental dot', async ({ page, appURL, context }) => {
  await openDrawing(page,appURL);
  const cdp=await context.newCDPSession(page);
  await cdp.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[{x:300,y:240,id:1},{x:400,y:240,id:2}]});
  await cdp.send('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:[{x:250,y:270,id:1},{x:450,y:270,id:2}]});
  await cdp.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
  await expect(page.locator('#zoom-level')).toHaveText('200%');await saved(page);
  await page.getByRole('button',{name:'처음 위치',exact:true}).click();await saved(page);
  await expect.poll(()=>pixels(page,[[300,240],[400,240],[250,270],[450,270]])).toEqual([0,0,0,0]);
  await expect(page.locator('#undo')).toBeDisabled();
  // One finger still draws normally after all pinch pointers have lifted.
  await cdp.send('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[{x:300,y:240,id:1}]});
  await cdp.send('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});await saved(page);
  await expect.poll(()=>pixels(page,[[300,240]])).toEqual([255]);
});

test('portrait and landscape participants share world coordinates', async ({ page, otherPage, appURL }, testInfo) => {
  await page.setViewportSize({width:390,height:844});
  await Promise.all([openDrawing(page,appURL),openDrawing(otherPage,appURL)]);
  const other=otherPage.viewportSize();
  await stroke(page,[145,422],[245,422]);
  await expect.poll(()=>pixels(otherPage,[[other.width/2,other.height/2]])).toEqual([255]);
  await page.screenshot({path:testInfo.outputPath('portrait.png')});
  await page.setViewportSize({width:844,height:390});await saved(page);
  await expect.poll(()=>pixels(page,[[422,195]])).toEqual([255]);
});

test('local storage failure blocks sending and retry preserves the in-memory work', async ({ page, otherPage, appURL }) => {
  await Promise.all([openDrawing(page,appURL),openDrawing(otherPage,appURL)]);
  await page.evaluate(()=>{
    window.originalPut=IDBObjectStore.prototype.put;
    IDBObjectStore.prototype.put=function(){throw new DOMException('Test quota failure','QuotaExceededError');};
  });
  await page.mouse.click(300,240);
  await expect(page.locator('#status')).toHaveText(/기기에 작업을 보관하지 못/);
  await expect.poll(()=>pixels(otherPage,[[300,240]])).toEqual([0]);
  await expect(page.locator('#export')).toBeVisible();
  await page.evaluate(()=>{IDBObjectStore.prototype.put=window.originalPut;});
  await page.getByRole('button',{name:'다시 시도',exact:true}).click();await saved(page);
  await expect.poll(()=>pixels(otherPage,[[300,240]])).toEqual([255]);
});

test('initial readiness is stable when the scheduled camera refresh elapses', async ({ page, appURL }) => {
  await page.clock.install({time:new Date('2026-01-01T00:00:00Z')});
  await page.clock.pauseAt(new Date('2026-01-01T00:00:01Z'));
  await page.addInitScript(()=>{
    window.viewRequests=0;
    const send=WebSocket.prototype.send;
    WebSocket.prototype.send=function(data){
      if(JSON.parse(new TextDecoder().decode(data)).type==='view') window.viewRequests++;
      return send.call(this,data);
    };
  });
  await openDrawing(page,appURL);
  // Advance past the 80 ms debounce, independent of browser/CI scheduling.
  await page.clock.runFor(100);
  await expect(page.locator('#status')).toHaveText('저장됨');
  expect(await page.evaluate(()=>window.viewRequests)).toBe(1);
});

test('unsupported WebGL2 shows guidance without starting input or a connection', async ({ page, appURL }) => {
  let connections=0;
  page.on('websocket',()=>connections++);
  await page.addInitScript(()=>{
    const getContext=HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext=function(type,...args){return type==='webgl2'?null:getContext.call(this,type,...args);};
  });
  await page.goto(appURL);
  await expect(page.locator('#status')).toHaveText(/WebGL2/);
  await expect(page.locator('#controls')).toBeHidden();
  await page.mouse.click(300,240);await page.keyboard.press('b');
  expect(connections).toBe(0);
});
