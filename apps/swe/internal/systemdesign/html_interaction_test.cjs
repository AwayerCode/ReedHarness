// Run: node --test apps/swe/internal/systemdesign/html_interaction_test.cjs
const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'architecture.html.tmpl'),'utf8');
const script=source.match(/<script type="application\/javascript">([\s\S]*?)<\/script>/)[1];
function element(dataset,classes=[]) {
 const names=new Set(classes), events={};
 return {dataset,classList:{contains:n=>names.has(n),toggle(n,on){if(on)names.add(n);else names.delete(n);}},
 addEventListener(n,fn){events[n]=fn;},fire(n,event){events[n](event);}};
}
function fixture(){
 const nodes=['up','selected','down','indirect','isolated'].map(module=>element({module}));
 const edges=[['up','selected','module'],['selected','down','module'],['down','indirect','module'],['selected','down','layer']].map(([from,to,kind])=>element({from,to},['edge-'+kind]));
 const svg=element({}),status={textContent:'idle'};
 svg.querySelectorAll=s=>s==='.node-link'?nodes:edges;
 vm.runInNewContext(script,{document:{querySelector:()=>svg,getElementById:()=>status}});
 return {nodes,edges,svg,status};
}
test('hover highlights direct relationships only, then restores',()=>{
 const {nodes,edges,svg,status}=fixture();
 nodes[1].fire('pointerenter');
 assert.equal(svg.classList.contains('tracing'),true);
 assert.deepEqual(nodes.map(n=>n.classList.contains('related')),[true,true,true,false,false]);
 assert.deepEqual(edges.map(n=>n.classList.contains('related')),[true,true,false,false]);
 assert.equal(nodes[1].classList.contains('selected'),true);
 assert.match(status.textContent,/1 个上游.*1 个下游/);
 nodes[1].fire('pointerleave');
 assert.equal(svg.classList.contains('tracing'),false);
 assert.equal(status.textContent,'idle');
 assert.ok([...nodes,...edges].every(n=>!n.classList.contains('related')));
});
test('hover overrides focus and falls back to focused module',()=>{
 const {nodes,svg}=fixture();
 nodes[1].fire('focus');nodes[2].fire('pointerenter');
 assert.equal(nodes[2].classList.contains('selected'),true);
 nodes[2].fire('pointerleave');
 assert.equal(nodes[1].classList.contains('selected'),true);
 nodes[1].fire('blur');assert.equal(svg.classList.contains('tracing'),false);
});
test('isolated module is visible without unrelated edges',()=>{
 const {nodes,edges,status}=fixture();nodes[4].fire('pointerenter');
 assert.deepEqual(nodes.map(n=>n.classList.contains('related')),[false,false,false,false,true]);
 assert.ok(edges.every(n=>!n.classList.contains('related')));
 assert.match(status.textContent,/0 个上游.*0 个下游/);
});

test('zoom fits both dimensions, preserves manual zoom on resize, and restores fit',()=>{
 const zoomScript=[...source.matchAll(/<script type="application\/javascript">([\s\S]*?)<\/script>/g)][1][1];
 const svg={viewBox:{baseVal:{width:2000,height:1600}},style:{}};
 const canvas={...element({}),style:{setProperty(){}},clientWidth:1000,clientHeight:800,scrollLeft:0,scrollTop:0,querySelector:()=>svg};
 const controls=Object.fromEntries(['zoom-level','zoom-in','zoom-out','zoom-reset','zoom-fit'].map(id=>[id,element({})]));
 let resize;
 vm.runInNewContext(zoomScript,{document:{querySelector:()=>canvas,getElementById:id=>controls[id]},ResizeObserver:class{constructor(fn){resize=fn;}observe(){}}});
 assert.ok(parseFloat(svg.style.width)<=1000);
 assert.ok(parseFloat(svg.style.height)<=800);
 controls['zoom-reset'].fire('click');
 assert.equal(svg.style.width,'2000px');
 canvas.getBoundingClientRect=()=>({left:0,top:0});
 const before=(200-parseFloat(svg.style.left));
 canvas.fire('wheel',{preventDefault(){},deltaY:-100,deltaMode:0,clientX:200,clientY:150});
 const after=(200-parseFloat(svg.style.left))/(parseFloat(svg.style.width)/2000);
 assert.ok(Math.abs(before-after)<1e-8,'wheel keeps the pointer anchor fixed');
 canvas.setPointerCapture=()=>{};canvas.hasPointerCapture=()=>false;
 const oldX=parseFloat(svg.style.left);
 canvas.fire('pointerdown',{button:0,isPrimary:true,pointerId:1,clientX:200,clientY:150});
 canvas.fire('pointermove',{pointerId:1,clientX:240,clientY:180,preventDefault(){}});
 assert.equal(parseFloat(svg.style.left),oldX+40);
 canvas.fire('pointerup',{pointerId:1});
 let blocked=false;
 canvas.fire('click',{detail:1,preventDefault(){},stopImmediatePropagation(){blocked=true;}});
 assert.equal(blocked,true);
 controls['zoom-reset'].fire('click');
 canvas.clientWidth=700;resize();assert.equal(svg.style.width,'2000px');
 controls['zoom-in'].fire('click');assert.equal(svg.style.width,'2500px');
 controls['zoom-out'].fire('click');assert.equal(svg.style.width,'2000px');
 controls['zoom-fit'].fire('click');assert.ok(parseFloat(svg.style.width)<=700);
 canvas.clientHeight=400;resize();assert.ok(parseFloat(svg.style.height)<=400);
 canvas.clientWidth=4000;canvas.clientHeight=3000;resize();assert.equal(svg.style.width,'2000px');
 assert.equal(controls['zoom-level'].textContent,'100%');

});

test('module details open in modal without navigation and restore focus on close',()=>{
 const detailScript=[...source.matchAll(/<script type="application\/javascript">([\s\S]*?)<\/script>/g)][2][1];
 const node=element({module:'orders'}),dialog=element({}),content=element({}),close=element({});
 let rendered,focused,prevented=false;
 content.replaceChildren=value=>{rendered=value;};
 dialog.showModal=()=>{dialog.open=true;};
 dialog.close=()=>{dialog.open=false;dialog.fire('close');};
 node.focus=options=>{focused=options.preventScroll;};
 const menu=element({}),action=element({});menu.hidden=true;menu.style={};menu.offsetWidth=180;menu.offsetHeight=40;action.focus=()=>{};
 const ids={'module-menu':menu,'menu-details':action,'module-dialog':dialog,'detail-content':content,'detail-close':close,'module-detail-title':{},'info-orders':{dataset:{name:'orders'},content:{cloneNode:()=> 'order details'}}};
 vm.runInNewContext(detailScript,{window:{innerWidth:800,innerHeight:600,addEventListener(){}},document:{getElementById:id=>ids[id],querySelectorAll:()=>[node],addEventListener(){}}});
 node.fire('click',{preventDefault(){prevented=true;}});
 assert.equal(prevented,true);assert.notEqual(dialog.open,true);
 node.fire('contextmenu',{preventDefault(){},clientX:790,clientY:590});
 assert.equal(menu.hidden,false);assert.equal(menu.style.left,'612px');assert.equal(menu.style.top,'552px');
 action.fire('click');assert.equal(menu.hidden,true);assert.equal(dialog.open,true);assert.equal(rendered,'order details');
 close.fire('click');assert.equal(dialog.open,false);assert.equal(focused,true);
 node.fire('contextmenu',{preventDefault(){},clientX:100,clientY:100});action.fire('click');
 dialog.getBoundingClientRect=()=>({left:100,right:500,top:100,bottom:500});
 const inside={target:dialog,clientX:120,clientY:120};
 const outside={target:dialog,clientX:50,clientY:50};
 dialog.fire('pointerdown',inside);dialog.fire('click',outside);
 assert.equal(dialog.open,true,'dragging out of the dialog must not close it');
 dialog.fire('pointerdown',outside);dialog.fire('click',outside);
 assert.equal(dialog.open,false,'backdrop click closes the dialog');
});

test('column dividers resize adjacent columns with minimum widths and keyboard support',()=>{
 const dividerScript=[...source.matchAll(/<script type="application\/javascript">([\s\S]*?)<\/script>/g)][3][1];
 const content=element({}),dialog=element({}),handle=element({divider:'1'});
 const classes={add(){},remove(){}};
 const grid={style:{},classList:classes,querySelectorAll:()=>[300,400,300].map(width=>({getBoundingClientRect:()=>({width})}))};
 handle.parentElement=grid;handle.classList=classes;handle.focus=()=>{};handle.setPointerCapture=()=>{};handle.hasPointerCapture=()=>false;handle.setAttribute=()=>{};
 const target={closest:()=>handle};
 vm.runInNewContext(dividerScript,{document:{getElementById:id=>id==='detail-content'?content:dialog}});
 content.fire('pointerdown',{target,button:0,isPrimary:true,pointerId:1,clientX:200,preventDefault(){}});
 content.fire('pointermove',{pointerId:1,clientX:250});
 assert.equal(grid.style.gridTemplateColumns,'300px 7px 450px 7px 250px');
 content.fire('pointermove',{pointerId:1,clientX:2000});
 assert.equal(grid.style.gridTemplateColumns,'300px 7px 520px 7px 180px');
 content.fire('pointerup',{pointerId:1});
 content.fire('keydown',{target,key:'ArrowLeft',shiftKey:false,preventDefault(){}});
 assert.equal(grid.style.gridTemplateColumns,'300px 7px 390px 7px 310px');
});

test('theme follows system in auto mode and preserves manual choice',()=>{
 const themeScript=[...source.matchAll(/<script type="application\/javascript">([\s\S]*?)<\/script>/g)][4][1];
 const select=element({});select.value='auto';
 const system=element({});system.matches=true;
 const root={dataset:{}};let saved='auto';
 vm.runInNewContext(themeScript,{document:{getElementById:()=>select,documentElement:root},window:{matchMedia:()=>system},localStorage:{getItem:()=>saved,setItem:(k,v)=>{saved=v;}}});
 assert.equal(root.dataset.theme,'dark');
 system.matches=false;system.fire('change');assert.equal(root.dataset.theme,'light');
 select.value='dark';select.fire('change');assert.equal(saved,'dark');
 system.fire('change');assert.equal(root.dataset.theme,'dark');
 select.value='auto';select.fire('change');assert.equal(root.dataset.theme,'light');
});

test('edge filters respect persistent selection',()=>{
 const searchScript=[...source.matchAll(/<script type="application\/javascript">([\s\S]*?)<\/script>/g)][5][1];
 const nodes=['inventory','orders'].map(module=>element({module}));
 const edges=[element({from:'inventory',to:'orders'},['edge-module']),element({from:'other',to:'orders'},['edge-module']),element({from:'domain',to:'ports'},['edge-layer'])];
 const svg={querySelectorAll:s=>s==='.node-link'?nodes:edges,dispatchEvent:event=>{svg.selection=event.detail;}};
 const canvas={...element({}),querySelector:()=>svg,dispatchEvent:event=>{canvas.located=event.detail;}};
 const ids=Object.fromEntries(['edge-filter'].map(id=>[id,element({})]));
 ids['edge-filter'].value='all';
 vm.runInNewContext(searchScript,{CustomEvent:class{constructor(type,options){this.type=type;this.detail=options.detail;}},document:{querySelector:()=>canvas,getElementById:id=>ids[id],addEventListener(){},createElement(){const b=element({});b.click=()=>b.fire('click');return b;}}});
 nodes[0].fire('click');assert.equal(svg.selection,'inventory');
 ids['edge-filter'].value='related';ids['edge-filter'].fire('change');
 assert.deepEqual(edges.map(e=>e.classList.contains('filtered')),[false,true,true]);
 canvas.fire('click',{target:{closest:()=>null}});assert.ok(edges.every(e=>e.classList.contains('filtered')));assert.equal(svg.selection,null);
 ids['edge-filter'].value='layer';ids['edge-filter'].fire('change');assert.deepEqual(edges.map(e=>e.classList.contains('filtered')),[true,true,false]);
 ids['edge-filter'].value='module';ids['edge-filter'].fire('change');assert.deepEqual(edges.map(e=>e.classList.contains('filtered')),[false,false,true]);
 ids['edge-filter'].value='all';ids['edge-filter'].fire('change');assert.ok(edges.every(e=>!e.classList.contains('filtered')));
});
