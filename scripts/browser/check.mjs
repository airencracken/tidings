import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:net';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.TIDINGS_PLAYWRIGHT_PATH
  ? pathToFileURL(process.env.TIDINGS_PLAYWRIGHT_PATH).href : 'playwright');
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const moduleDir = spawnSync('go',['list','-m','-f','{{.Dir}}','github.com/airencracken/comfylib'],{cwd:root,encoding:'utf8'});
assert.equal(moduleDir.status,0,moduleDir.stderr);
const { accountLayout,accountWidths,invitationLayout,invitationWidths,assertInvitationLayout } = await import(pathToFileURL(path.join(moduleDir.stdout.trim(),'ui/uitest/layout.mjs')).href);
const temporary = await mkdtemp(path.join(tmpdir(), 'tidings-browser-'));
const password = 'quiet mornings and books';
const passwordFile = path.join(temporary, 'password');
await writeFile(passwordFile, password + '\n', { mode: 0o600 });
const reservation=createServer();
await new Promise(resolve=>reservation.listen(0,'127.0.0.1',resolve));
const port=reservation.address().port;
await new Promise(resolve=>reservation.close(resolve));
const localOrigin='http://127.0.0.1:'+port;
const env = { ...process.env, TIDINGS_DATA_DIR: path.join(temporary, 'data'), TIDINGS_ADDR: '127.0.0.1:'+port, TIDINGS_BASE_URL: localOrigin, TIDINGS_SECURE_COOKIES: '' };
const binary = path.join(root, 'bin/tidings');
let server, browser;
const errors = [];
const observed = new Set();
async function signIn(page, origin, username='host') {
  await page.goto(origin + '/login');
  await page.getByLabel('Username', { exact:true }).fill(username);
  await page.getByLabel('Password', { exact:true }).fill(password);
  await Promise.all([page.waitForURL(origin + '/'), page.getByRole('button', { name:'Sign in',exact:true }).click()]);
}
try {
  const owner = spawnSync(binary, ['create-owner','--username','host','--password-file',passwordFile], { env, encoding:'utf8' });
  assert.equal(owner.status, 0, owner.stderr);
  const seeded = spawnSync('python3', ['-c', `
import sqlite3,sys,time
db=sqlite3.connect(sys.argv[1]);now=int(time.time())
db.execute("INSERT INTO feeds(id,url,title,checked) VALUES(1,'https://example.org/test-feed','Local test source',?)",(now,))
db.execute("INSERT INTO subscriptions(user_id,feed_id,category) VALUES(1,1,'AI')")
db.execute("INSERT INTO subscription_tags(user_id,feed_id,tag) VALUES(1,1,'research')")
db.execute("INSERT INTO feeds(id,url,title,checked) VALUES(2,'https://example.org/shared-test-feed','Household test source',?)",(now,))
db.execute("INSERT INTO group_feeds(feed_id,category) VALUES(2,'Infosec')")
db.execute("INSERT INTO household_tags(feed_id,tag) VALUES(2,'security')")
for i in range(35):
 db.execute("INSERT INTO entries(feed_id,key,title,url,body,published) VALUES(1,?,?,?,?,?)",(str(i),'Test story %02d: understanding our tools'%i,'https://example.org/story/'+str(i),'A closer look at the tools we use, the ideas behind them, and the small discoveries that make everyday work more interesting.',now-i))
db.execute("INSERT INTO entries(feed_id,key,title,url,body,published) VALUES(2,'shared','A shared test story','https://example.org/shared-story','A household browser-test fixture.',?)",(now,))
db.execute("UPDATE entries SET body_html=? WHERE id=1",('<h2>Useful details</h2><p>An article with a <a href="https://example.org/reference?utm_source=feed&amp;id=4">reference</a>.</p><ul><li>A list item</li></ul><blockquote>A useful quotation.</blockquote><pre><code>  indented code</code></pre><img src="https://example.org/preview.png" alt="A cached illustration"><script>throw new Error("Publisher script ran")</script>',))
db.execute("UPDATE entries SET image_url='https://example.org/preview.png' WHERE id=1")
db.commit()
`, path.join(env.TIDINGS_DATA_DIR,'reader.db')], { encoding:'utf8' });
  assert.equal(seeded.status, 0, seeded.stderr);
  server = spawn(binary, ['serve'], { env, stdio:['ignore','pipe','pipe'] });
  let output = '';
  const origin = await new Promise((resolve,reject) => {
    const timeout = setTimeout(() => reject(new Error('Server did not become ready: '+output)),15000);
    server.once('exit', code => {clearTimeout(timeout);reject(new Error('Server exited '+code+': '+output));});
    const capture = chunk => { output += chunk; const match=output.match(/url=(http:\/\/127\.0\.0\.1:\d+)/);if(match){clearTimeout(timeout);resolve(match[1]);} };
    server.stdout.on('data',capture);server.stderr.on('data',capture);
  });
  assert.equal((await fetch(origin + '/healthz')).status,200);
  browser = await chromium.launch({ headless:true, executablePath: process.env.CHROMIUM || '/usr/bin/chromium' });
  const context = await browser.newContext({ viewport:{width:1440,height:1000} });
  context.on('request',request => observed.add(request.url()));
  const page=await context.newPage();page.on('pageerror',error => errors.push(String(error)));
  for(const icon of ['/favicon.ico','/static/favicon.svg','/static/apple-touch-icon.png']) {
    const response=await context.request.get(origin+icon);
    assert.equal(response.status(),200,`public favicon ${icon}`);
    assert.ok(!response.headers()['set-cookie'],'favicon starts an anonymous session');
  }

  const preview = await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width=640;canvas.height=360;
    const context=canvas.getContext('2d');context.fillStyle='#dce6cf';context.fillRect(0,0,640,360);
    context.fillStyle='#355c48';context.fillRect(80,60,480,240);context.fillStyle='#fffef9';context.font='36px Georgia';context.fillText('A quieter reading table',135,195);
    return canvas.toDataURL('image/jpeg').split(',')[1];
  });
  const cacheDirectory=path.join(env.TIDINGS_DATA_DIR,'cache','images');await mkdir(cacheDirectory,{recursive:true,mode:0o700});
  await writeFile(path.join(cacheDirectory,createHash('sha256').update('https://example.org/preview.png').digest('hex')+'.jpg'),Buffer.from(preview,'base64'),{mode:0o600});
  await signIn(page,origin);
  assert.equal(await page.locator('link[rel="icon"][type="image/svg+xml"]').getAttribute('href'),'/static/favicon.svg');
  assert.equal(await page.locator('link[rel="apple-touch-icon"]').getAttribute('href'),'/static/apple-touch-icon.png');
  await page.evaluate(async()=>{const icon=new Image();icon.src='/static/favicon.svg';await icon.decode();if(!icon.naturalWidth)throw new Error('SVG favicon did not render');});

  // A saved theme must be applied before the body is parsed, even when the
  // operating-system preference is the opposite and script delivery is slow.
  for (const [saved,system] of [['dark','light'],['light','dark']]) {
    const firstPaintContext=await browser.newContext({colorScheme:system});
    await firstPaintContext.addInitScript(value=>{
      localStorage.setItem('tidings-theme',value);
      new MutationObserver((_,observer)=>{
        if(document.body){window.firstBodyTheme=document.documentElement.dataset.theme;observer.disconnect();}
      }).observe(document,{childList:true,subtree:true});
    },saved);
    const firstPaint=await firstPaintContext.newPage();
    await firstPaint.route('**/static/theme.js',async route=>{await new Promise(resolve=>setTimeout(resolve,150));await route.continue();});
    for(const route of ['/login','/join','/login']) {
      await firstPaint.goto(origin+route);
      assert.equal(await firstPaint.evaluate(()=>window.firstBodyTheme),saved,'theme changed after body parsing on '+route);
      assert.equal(await firstPaint.locator('html').getAttribute('data-theme'),saved);
    }
    await firstPaintContext.close();
  }
  assert.ok(await page.locator('[data-fetch-status]').isVisible());
  const statusResponse=await context.request.get(origin+'/api/v1/fetch-status');assert.equal(statusResponse.status(),200);
  assert.ok((await statusResponse.json()).next_check.startsWith('Next automatic check'));
  const initialCheck=Number(await page.locator('[data-fetch-status]').getAttribute('data-checked'));
  let fetchStage=0;
  await page.route('**/api/v1/fetch-status',route => route.fulfill({json:{state:['queued','fetching','idle'][Math.min(fetchStage++,2)],message:['Your sources are queued for fetching.','Fetching your sources.','Feeds checked just now.'][Math.min(fetchStage-1,2)],detail:'',checked:initialCheck+1,next_check:'Next automatic check in 30 minutes.'}}));
  await page.reload();
  await page.getByText('Your sources are queued for fetching.',{exact:true}).waitFor();
  await page.getByText('Fetching your sources.',{exact:true}).waitFor();
  await page.getByText('Feeds checked just now.',{exact:true}).waitFor();
  assert.ok(await page.getByRole('link',{name:'Show updated stories',exact:true}).isVisible());
  await page.unroute('**/api/v1/fetch-status');
  await page.goto(origin+'/account');
  assert.equal(await page.getByLabel('Show feed images',{exact:true}).isChecked(),false);
  await page.getByLabel('Show feed images',{exact:true}).check();await page.getByRole('button',{name:'Save image settings',exact:true}).click();
  await page.goto(origin+'/');
  await page.locator('[data-feed-image]').first().evaluate(image=>image.decode());
  assert.equal((await context.request.get(origin+'/story/1/image')).status(),200);
  await page.goto(origin+'/story/1');assert.equal(await page.locator('.story-hero img').count(),1);
  assert.equal(await page.locator('.reader-body h2').textContent(),'Useful details');
  assert.equal(await page.locator('.reader-body pre code').textContent(),'  indented code');
  assert.equal(await page.locator('.reader-body a').getAttribute('href'),'https://example.org/reference?id=4');
  await page.locator('.reader-body img').evaluate(image=>image.decode());
  assert.ok((await page.locator('.reader-body img').getAttribute('src')).startsWith('/story/1/image/'));
  assert.equal(await page.locator('.reader-body script').count(),0);
  assert.ok(!(await page.locator('body').innerText()).includes('The text supplied by this feed'));
  await page.goto(origin+'/account');await page.getByLabel('Show feed images',{exact:true}).uncheck();await page.getByRole('button',{name:'Save image settings',exact:true}).click();
  assert.equal((await context.request.get(origin+'/story/1/image')).status(),404);
  await page.goto(origin+'/story/1');assert.equal(await page.locator('.reader-body img').count(),0);
  assert.equal(await page.getByRole('button',{name:'Get full story',exact:true}).count(),1);
  await page.goto(origin+'/');
  assert.equal(await page.locator('.story-row').count(),30);
  await page.getByRole('link',{name:'Next page',exact:true}).click();
  assert.equal(await page.locator('.story-row').count(),5);
  await page.goto(origin+'/');
  await page.locator('.story-row').first().getByRole('button',{name:'Set aside',exact:true}).click();
  await page.getByRole('link',{name:'Set aside',exact:true}).click();
  assert.equal(await page.locator('.story-row').count(),1);
  await page.locator('.story-row h2 a').first().click();
  await page.getByRole('button',{name:'Mark read and return',exact:true}).click();
  assert.equal(await page.locator('.story-row').count(),30);
  await page.getByRole('link',{name:'Feed groups',exact:true}).click();
  await page.getByLabel('Name',{exact:true}).fill('AI research');
  await page.getByLabel('How it gathers feeds').selectOption('tag');
  await page.getByLabel('Tag to match, for a tag-based group').fill('research');
  await page.getByRole('button',{name:'Create feed group',exact:true}).click();
  await page.getByRole('link',{name:'Read this group',exact:true}).click();
  assert.equal(await page.locator('.story-row').count(),30);
  await page.getByRole('link',{name:'Your account',exact:true}).click();
  assert.equal(await page.getByLabel('Follow the household table').isChecked(),false);
  await page.getByLabel('Follow the household table').check();
  await page.getByRole('button',{name:'Save your choice',exact:true}).click();
  await page.goto(origin+'/?view=group');
  assert.equal(await page.getByRole('heading',{name:'A shared test story',exact:true}).count(),1);

  // The host enables categories without subscribing any household member.
  await page.goto(origin+'/sources');
  assert.equal(await page.locator('.catalog-card').count(),0);
  await page.goto(origin+'/account');
  for(const category of ['Tech','Science']) await page.locator(`input[name="category"][value="${category}"]`).check();
  await page.getByRole('button',{name:'Save suggested categories',exact:true}).click();
  await page.goto(origin+'/sources');
  assert.ok(await page.locator('.catalog-card').count()>0);
  assert.equal(await page.getByRole('heading',{name:'Simon Willison',exact:true}).count(),0);
  await page.goto(origin+'/account/invitations');
  for(const theme of ['light','dark']) {
    await page.getByLabel('Theme',{exact:true}).selectOption(theme);
    for(const width of invitationWidths) {
      await page.setViewportSize({width,height:1000});
      assertInvitationLayout(await page.evaluate(invitationLayout),width,theme+' invitations');
    }
  }
  await page.setViewportSize({width:1440,height:1000});

  // An invitation is redeemed in a genuinely separate browser context.
  await page.goto(origin+'/account');
  await page.getByRole('button',{name:'Create an invitation',exact:true}).click();
  const invitation = await page.locator('#invitation').inputValue();
  const newcomer=await browser.newContext();
  const member=await newcomer.newPage();
  assert.ok(invitation.startsWith(origin+"/join?code="));
  await member.goto(invitation);
  await member.getByLabel('Choose a username').fill('alice');
  await member.getByLabel('Choose a password').fill(password);
  await member.getByRole('button',{name:'Make yourself at home',exact:true}).click();
  await signIn(member,origin,'alice');
  assert.equal(await member.locator('.story-row').count(),0);
  await member.goto(origin+'/?view=saved');
  assert.equal(await member.locator('.story-row').count(),0);
  await member.goto(origin+'/account');
  assert.equal(await member.getByLabel('Follow the household table').isChecked(),false);
  await member.getByLabel('Follow the household table').check();
  await member.getByRole('button',{name:'Save your choice',exact:true}).click();
  await member.goto(origin+'/?view=group');
  assert.equal(await member.locator('.story-row').count(),1);
  await member.locator('.story-row').getByRole('button',{name:'Mark read',exact:true}).click();
  await page.goto(origin+'/?view=group');
  assert.equal(await page.getByRole('button',{name:'Mark read',exact:true}).count(),1);


  const exported=await context.request.get(origin+'/api/v1/export');
  const data=await exported.json();
  assert.equal(data.username,'host');assert.equal(data.version,1);assert.equal(data.stories.length,1);assert.equal(data.feed_groups[0].name,'AI research');
  await member.goto(origin+'/account');
  await member.getByLabel('Tidings JSON file, up to 16 MiB').setInputFiles({name:'tidings.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(data))});
  await member.getByLabel('Merge this file into my personal reading data').check();
  await member.getByRole('button',{name:'Merge reading data',exact:true}).click();
  await member.goto(origin+'/?view=saved');
  assert.equal(await member.locator('.story-row').count(),1);
  await member.goto(origin+'/account');
  assert.equal(await member.getByRole('heading',{name:'Instance backups',exact:true}).count(),0);
  assert.equal(await member.getByRole('button',{name:'Fetch all feeds now',exact:true}).count(),0);
  assert.equal(await member.getByRole('button',{name:'Save suggested categories',exact:true}).count(),0);
  await page.goto(origin+'/account');
  let alice=page.locator('details.admin-row').filter({has:page.locator('summary').filter({hasText:'alice'})});
  await alice.locator('summary').click();
  await alice.getByLabel('Your current password',{exact:true}).fill(password);
  await alice.getByRole('button',{name:'Apply member action',exact:true}).click();
  await member.goto(origin+'/');
  assert.equal(new URL(member.url()).pathname,'/login');
  alice=page.locator('details.admin-row').filter({has:page.locator('summary').filter({hasText:'alice'})});
  await alice.locator('summary').click();
  await alice.getByLabel('Your current password',{exact:true}).fill(password);
  await alice.getByRole('button',{name:'Apply member action',exact:true}).click();
  await signIn(member,origin,'alice');
  await newcomer.close();
  await page.goto(origin+'/');
  assert.equal(await page.locator('script[src="/static/keyboard.js"]').count(),0);
  await page.keyboard.press('j');assert.equal(await page.locator('.keyboard-selected').count(),0);
  await page.goto(origin+'/account');
  assert.equal(await page.getByLabel('Enable keyboard shortcuts',{exact:true}).isChecked(),false);
  await page.getByLabel('Enable keyboard shortcuts',{exact:true}).check();
  await page.getByRole('button',{name:'Save reading preferences',exact:true}).click();
  await page.goto(origin+'/');
  await page.keyboard.press('j');assert.equal(await page.locator('.keyboard-selected').count(),1);
  const firstStory=await page.locator('.keyboard-selected [data-story-open]').getAttribute('href');
  await page.keyboard.press('j');assert.notEqual(await page.locator('.keyboard-selected [data-story-open]').getAttribute('href'),firstStory);
  await page.keyboard.press('k');assert.equal(await page.locator('.keyboard-selected [data-story-open]').getAttribute('href'),firstStory);
  await page.keyboard.press('?');assert.equal(await page.locator('#keyboard-help').evaluate(dialog=>dialog.open),true);
  await page.keyboard.press('Escape');assert.equal(await page.locator('#keyboard-help').evaluate(dialog=>dialog.open),false);
  await page.keyboard.press('/');assert.equal(await page.locator('#search').evaluate(input=>input===document.activeElement),true);
  await page.keyboard.type('jkms');assert.equal(await page.locator('#search').inputValue(),'jkms');
  await page.locator('#search').fill('');await page.locator('#search').blur();
  const modifiers=await page.evaluate(()=>['ctrlKey','altKey','metaKey','isComposing'].map(flag=>{const event=new KeyboardEvent('keydown',{key:'j',bubbles:true,cancelable:true,[flag]:true});document.body.dispatchEvent(event);return event.defaultPrevented;}));
  assert.deepEqual(modifiers,[false,false,false,false]);
  await page.keyboard.press('j');
  const selectedStory=await page.locator('.keyboard-selected [data-story-open]').getAttribute('href');
  await Promise.all([page.waitForURL(origin+selectedStory),page.keyboard.press('Enter')]);
  await Promise.all([page.waitForNavigation(),page.keyboard.press('m')]);
  assert.equal(await page.getByRole('button',{name:'Mark unread',exact:true}).count(),1);
  await page.locator('body').click({position:{x:5,y:5}});
  await Promise.all([page.waitForNavigation(),page.keyboard.press('s')]);
  assert.equal(await page.getByRole('button',{name:'Remove from set aside',exact:true}).count(),1);
  await page.goto(origin+'/account');await page.getByLabel('Enable keyboard shortcuts',{exact:true}).uncheck();
  await page.getByRole('button',{name:'Save reading preferences',exact:true}).click();await page.goto(origin+'/');
  assert.equal(await page.locator('script[src="/static/keyboard.js"]').count(),0);
  await page.keyboard.press('j');assert.equal(await page.locator('.keyboard-selected').count(),0);
  await page.goto(origin+'/sources');
  await page.getByLabel('Search suggested sources',{exact:true}).fill('zzzzabsent');
  await page.getByRole('button',{name:'Find sources',exact:true}).click();
  assert.equal(await page.locator('.catalog-card').count(),0);
  await page.locator('summary').filter({hasText:'Edit tags and topic'}).first().click();
  await page.getByLabel('Topic',{exact:true}).last().fill('Research');
  await page.getByRole('button',{name:'Save topic',exact:true}).click();
  assert.equal(await page.locator('.source-row .eyebrow').first().textContent(),'Research');
  const screenshotDir = process.env.TIDINGS_SCREENSHOT_DIR;
  if(screenshotDir) await mkdir(screenshotDir,{recursive:true});
  const welcomeContext=await browser.newContext();
  const welcome=await welcomeContext.newPage();
  welcome.on('pageerror',error=>errors.push(String(error)));
  await welcome.goto(origin+'/login');
  for(const theme of ['light','dark']) {
    await welcome.getByLabel('Theme',{exact:true}).selectOption(theme);
    for(const width of [375,1440]) {
      await welcome.setViewportSize({width,height:1000});
      assert.ok(await welcome.locator('.welcome-mascot').evaluate(img=>img.complete&&img.naturalWidth>0),'mascot did not load');
      assert.ok(await welcome.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`welcome overflows at ${width} in ${theme}`);
      if(screenshotDir) await welcome.screenshot({path:path.join(screenshotDir,`welcome-${theme}-${width}.png`),fullPage:true});
    }
  }
  for(const width of accountWidths){await welcome.setViewportSize({width,height:1000});assert.ok(await welcome.evaluate(accountLayout),`shared account layout at ${width}`);}
  await welcomeContext.close();
  for (const theme of ['light','dark']) {
    await page.goto(origin+'/');
    await page.getByLabel('Theme',{exact:true}).selectOption(theme);
    for(const width of [375,768,1024,1440]) {
      await page.setViewportSize({width,height:1000});
      for(const route of ['/','/sources','/groups','/account','/account/invitations','/story/1']) {
        await page.goto(origin+route);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth<=innerWidth+1),`${route} overflows at ${width} in ${theme}`);
        if(screenshotDir && route==='/account/invitations' && [375,1440].includes(width))await page.screenshot({path:path.join(screenshotDir,`invitations-${theme}-${width}.png`),fullPage:true});
        if(screenshotDir && route==='/account' && [375,1440].includes(width))await page.screenshot({path:path.join(screenshotDir,`account-${theme}-${width}.png`),fullPage:true});
      }
      if(screenshotDir && (width===375 || width===1440)) {
        await page.goto(origin+'/');await page.screenshot({path:path.join(screenshotDir,`reader-${theme}-${width}.png`),fullPage:false});
      }
    }
  }
  const plainContext=await browser.newContext({javaScriptEnabled:false});
  const plain=await plainContext.newPage();
  await plain.goto(origin+'/login');for(const width of accountWidths){await plain.setViewportSize({width,height:1000});assert.ok(await plain.evaluate(accountLayout),`shared account layout without JavaScript at ${width}`);}
  await signIn(plain,origin);
  assert.equal(await plain.getByRole('heading',{name:'Your reading table',exact:true}).count(),1);
  await plain.goto(origin+'/groups');
  await plain.getByLabel('Name',{exact:true}).fill('Without JavaScript');
  await plain.getByRole('button',{name:'Create feed group',exact:true}).click();
  await plain.getByLabel('Local test source',{exact:false}).check();
  await plain.getByRole('button',{name:'Save chosen feeds',exact:true}).click();
  await plain.getByRole('link',{name:'Read this group',exact:true}).click();
  assert.equal(await plain.locator('.story-row').count(),30);
  await plain.goto(origin+'/groups');
  await plain.getByRole('link',{name:'Without JavaScript',exact:true}).click();
  await plain.getByLabel('Name',{exact:true}).fill('Quiet reading');
  await plain.getByRole('button',{name:'Save group name',exact:true}).click();
  assert.equal(await plain.getByRole('link',{name:'Quiet reading',exact:true}).count(),1);
  await plainContext.close();
  assert.deepEqual(errors,[]);
  assert.ok([...observed].every(address => address.startsWith(origin+'/')),'browser contacted a third party');
  console.log('Browser checks passed: optional keyboard shortcuts, rich articles, tracking cleanup, cached inline images, live fetch status, cached feed images, accounts, invitation, isolated state, household opt-in, feed groups, exports, pagination, JavaScript-free forms, both themes, and five routes at four widths.');
} finally {
  if(browser) await browser.close();
  if(server && server.exitCode===null){server.kill('SIGTERM');await Promise.race([once(server,'exit'),new Promise(resolve=>setTimeout(resolve,5000))]);if(server.exitCode===null)server.kill('SIGKILL');}
  await rm(temporary,{recursive:true,force:true});
}
