import {chromium} from '../../qa/node_modules/playwright/index.mjs';
import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
const html=await fs.readFile(new URL('./home.html',import.meta.url),'utf8');
const browser=await chromium.launch({channel:'chrome',headless:true});
const results=[];
try{
 for(const [name,token,available] of [['underscore','_valid_capability_nonce_for_ui_test',true],['placeholder','__SIDEKICKS_DESKTOP_TOKEN__',false]]){
  const page=await browser.newPage({viewport:{width:1100,height:430}});let request=null;
  await page.route('http://127.0.0.1:17888/**',async route=>{
   if(route.request().url().endsWith('/desktop/launch')){request={body:route.request().postDataJSON(),token:route.request().headers()['x-sidekicks-desktop']};await route.fulfill({contentType:'application/json',body:'{"ok":true}'});}
   else await route.fulfill({contentType:'text/html',body:html.replaceAll('__SIDEKICKS_DESKTOP_TOKEN__',token)});
  });
  await page.goto('http://127.0.0.1:17888/home');
  for(const app of ['browser','terminal','files']){const b=page.locator('[data-app='+app+']');assert.equal(await b.isEnabled(),available);await b.scrollIntoViewIfNeeded();assert(await b.isVisible());}
  if(available){await page.locator('[data-app=terminal]').click();assert.deepEqual(request,{body:{app:'terminal'},token});}
  results.push({name,status:'PASS',scope:'isolated home-page DOM/nonce regression only; no native desktop proof'});await page.close();
 }
}finally{await browser.close();}
console.log(JSON.stringify(results,null,2));
