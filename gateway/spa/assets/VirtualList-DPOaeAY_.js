import{Y as e}from"./index-DmFVTDGA.js";import{r as t,j as i}from"./query-BDIvXWR3.js";
/**
 * @license lucide-react v0.468.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const r=e("Pause",[["rect",{x:"14",y:"4",width:"4",height:"16",rx:"1",key:"zuxfzm"}],["rect",{x:"6",y:"4",width:"4",height:"16",rx:"1",key:"1okwgv"}]]);function s({items:e,itemHeight:r,renderItem:s,containerHeight:n=400,overscan:o=5,className:a=""}){const l=t.useRef(null),[h,c]=t.useState(0),d=e.length*r,m=t.useMemo(()=>({startIdx:Math.max(0,Math.floor(h/r)-o),endIdx:Math.min(e.length,Math.ceil((h+n)/r)+o)}),[h,r,n,o,e.length]),x=t.useCallback(()=>{l.current&&c(l.current.scrollTop)},[]),u=t.useMemo(()=>{const{startIdx:t,endIdx:i}=m;return e.slice(t,i).map((e,i)=>({item:e,index:t+i}))},[e,m]);return i.jsx("div",{ref:l,onScroll:x,className:`overflow-auto ${a}`,style:{height:n},children:i.jsx("div",{style:{height:d,position:"relative"},children:u.map(({item:e,index:t})=>i.jsx("div",{style:{position:"absolute",top:t*r,height:r,left:0,right:0},children:s(e,t)},t))})})}export{r as P,s as V};
