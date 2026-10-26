export function downloadBlob(blob:Blob,name:string){
 const url=URL.createObjectURL(blob),link=document.createElement('a');link.href=url;link.download=name;
 document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);
}
export function downloadTorrent(data:string,name:string){
 const bytes=Uint8Array.from(atob(data),c=>c.charCodeAt(0));
 downloadBlob(new Blob([bytes],{type:'application/x-bittorrent'}),`${name.replace(/[<>:"/\\|?*\x00-\x1f]/g,'_').slice(0,140)||'torrent'}.torrent`);
}
