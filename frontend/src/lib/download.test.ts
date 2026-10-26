import {expect,test,vi} from 'vitest';
import {downloadTorrent} from './download';
test('exports exact metadata bytes with a safe filename and releases the object URL',()=>{
 vi.useFakeTimers();const create=vi.spyOn(URL,'createObjectURL').mockReturnValue('blob:test'),revoke=vi.spyOn(URL,'revokeObjectURL').mockImplementation(()=>{});
 let name='';const click=vi.spyOn(HTMLAnchorElement.prototype,'click').mockImplementation(function(this:HTMLAnchorElement){name=this.download;});
 try{downloadTorrent(btoa(String.fromCharCode(0,255,120)),'unsafe:/name');expect(name).toBe('unsafe__name.torrent');expect((create.mock.calls[0][0] as Blob).size).toBe(3);vi.advanceTimersByTime(1000);expect(revoke).toHaveBeenCalledWith('blob:test');}finally{click.mockRestore();create.mockRestore();revoke.mockRestore();vi.useRealTimers();}
});
