import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.TIDINGS_PLAYWRIGHT_PATH
  ? pathToFileURL(process.env.TIDINGS_PLAYWRIGHT_PATH).href : 'playwright');
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const moduleDir = spawnSync('go',['list','-m','-f','{{.Dir}}','github.com/airencracken/comfylib'],{cwd:root,encoding:'utf8'});
assert.equal(moduleDir.status,0,moduleDir.stderr);
const { accountLayout,accountWidths } = await import(pathToFileURL(path.join(moduleDir.stdout.trim(),'ui/uitest/layout.mjs')).href);
const temporary = await mkdtemp(path.join(tmpdir(), 'tidings-browser-'));
const password = 'quiet mornings and books';
const passwordFile = path.join(temporary, 'password');
await writeFile(passwordFile, password + '\n', { mode: 0o600 });
const env = { ...process.env, TIDINGS_DATA_DIR: path.join(temporary, 'data'), TIDINGS_ADDR: '127.0.0.1:0', TIDINGS_BASE_URL: '', TIDINGS_SECURE_COOKIES: '' };
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
 db.execute("INSERT INTO entries(feed_id,key,title,url,body,published) VALUES(1,?,?,?,?,?)",(str(i),'Test story %02d: understanding our tools'%i,'https://example.org/story/'+str(i),'This is a local browser-test fixture. A readable explanation with no scripts, embeds, or external images.',now-i))
db.execute("INSERT INTO entries(feed_id,key,title,url,body,published) VALUES(2,'shared','A shared test story','https://example.org/shared-story','A household browser-test fixture.',?)",(now,))
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
  browser = await chromium.launch({ headless:true });
  const context = await browser.newContext({ viewport:{width:1440,height:1000} });
  context.on('request',request => observed.add(request.url()));
  const page=await context.newPage();page.on('pageerror',error => errors.push(String(error)));
  for(const icon of ['/favicon.ico','/static/favicon.svg','/static/apple-touch-icon.png']) {
    const response=await context.request.get(origin+icon);
    assert.equal(response.status(),200,`public favicon ${icon}`);
    assert.ok(!response.headers()['set-cookie'],'favicon starts an anonymous session');
  }
  await signIn(page,origin);
  assert.equal(await page.locator('link[rel="icon"][type="image/svg+xml"]').getAttribute('href'),'/static/favicon.svg');
  assert.equal(await page.locator('link[rel="apple-touch-icon"]').getAttribute('href'),'/static/apple-touch-icon.png');
  await page.evaluate(async()=>{const icon=new Image();icon.src='/static/favicon.svg';await icon.decode();if(!icon.naturalWidth)throw new Error('SVG favicon did not render');});
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

  // An invitation is redeemed in a genuinely separate browser context.
  await page.goto(origin+'/account');
  await page.getByRole('button',{name:'Create an invitation',exact:true}).click();
  const invitation = await page.locator('#invitation').inputValue();
  const newcomer=await browser.newContext();
  const member=await newcomer.newPage();
  await member.goto(origin+invitation);
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
      for(const route of ['/','/sources','/groups','/account','/story/1']) {
        await page.goto(origin+route);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth<=innerWidth+1),`${route} overflows at ${width} in ${theme}`);
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
  console.log('Browser checks passed: accounts, invitation, isolated state, household opt-in, feed groups, exports, pagination, JavaScript-free forms, both themes, and five routes at four widths.');
} finally {
  if(browser) await browser.close();
  if(server && server.exitCode===null){server.kill('SIGTERM');await Promise.race([once(server,'exit'),new Promise(resolve=>setTimeout(resolve,5000))]);if(server.exitCode===null)server.kill('SIGKILL');}
  await rm(temporary,{recursive:true,force:true});
}
