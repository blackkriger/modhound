package crash

import (
	"strings"
	"testing"
	"time"
)

const sampleLog = `[00:57:48] [NEI Plugin Loader/INFO]: Bee Species Root found!
[00:57:48] [Client thread/ERROR]: Failed to Load blusunrize.immersiveengineering.client.nei.NEIConfig
java.lang.NoClassDefFoundError: blusunrize/immersiveengineering/client/nei/NEIArcFurnaceHandler (wrong name: NEIArcFurnaceHandlerAlloying)
	at java.lang.ClassLoader.defineClass1(Native Method)
	at blusunrize.immersiveengineering.client.nei.NEIArcFurnaceHandler$ProxyClassLoader.findClass(NEIArcFurnaceHandler.java:30) ~[NEIArcFurnaceHandler$ProxyClassLoader.class:?]
[00:57:49] [Client thread/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: java.lang.NullPointerException
[00:57:49] [Client thread/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: 	at moze_intel.projecte.gameObjs.items.ItemCharge.getCharge(ItemCharge.java:40)
[02:49:57] [NEI Plugin Loader/ERROR]: Exception caught during firing event codechicken.nei.event.NEIConfigsLoadedEvent@4c0e2fbb:
java.lang.NullPointerException
	at tconstruct.plugins.nei.RecipeHandlerToolMaterials.loadCraftingRecipes(RecipeHandlerToolMaterials.java:240) ~[RecipeHandlerToolMaterials.class:?]
	at thelm.packagedauto.integration.nei.NEIHandler.lambda$onNEIConfigsLoaded$6(NEIHandler.java:116) ~[NEIHandler.class:1.7.10-W.0.2.9]
[02:49:57] [NEI Plugin Loader/ERROR]: Index: 4 Listeners:
[02:49:57] [NEI Plugin Loader/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: java.lang.NullPointerException
[02:49:57] [NEI Plugin Loader/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: 	at tconstruct.plugins.nei.RecipeHandlerToolMaterials.loadCraftingRecipes(RecipeHandlerToolMaterials.java:240)
[03:00:00] [Server thread/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: java.lang.NoSuchMethodError: moze_intel.projecte.utils.EMCHelper.getEmcValue(Lnet/minecraft/item/ItemStack;)I
[03:00:00] [Server thread/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: 	at akuto2.peex.tile.TileEntityCondenserMk3.func_145845_h(TileEntityCondenserMk3.java:50)
[03:00:00] [Server thread/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: Caused by: java.lang.ClassNotFoundException: x.Y
[03:00:00] [Server thread/INFO]: [java.lang.Throwable$WrappedPrintStream:println:-1]: 	... 3 more
`

func TestParseLog(t *testing.T) {
	r := ParseLog(strings.NewReader(sampleLog))
	var got []string
	for _, b := range r.Blocks {
		got = append(got, b[0].Short()+"@"+b[0].Frames[len(b[0].Frames)-1])
	}
	want := []string{
		"NoClassDefFoundError@blusunrize/immersiveengineering/client/nei/NEIArcFurnaceHandler$ProxyClassLoader",
		"NullPointerException@thelm/packagedauto/integration/nei/NEIHandler",
		"NoSuchMethodError@akuto2/peex/tile/TileEntityCondenserMk3",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := len(r.Blocks[2]); n != 2 {
		t.Errorf("caused-by chain: got %d causes", n)
	}
}

func TestParseLogLongLinesAndMultilineMessages(t *testing.T) {
	text := "[10:00:00] [main/INFO]: " + strings.Repeat("x", 3<<20) + "\n" +
		"[10:00:01] [Server thread/ERROR]: Exception caught during firing event foo:\n" +
		"java.lang.IllegalStateException: first line\n" +
		"second line of the message\n" +
		"\tat com.mod.Handler.onEvent(Handler.java:10)\n" +
		"[10:00:02] [Server thread/INFO]: done\n"
	r := ParseLog(strings.NewReader(text))
	if len(r.Blocks) != 1 || len(r.Blocks[0][0].Frames) != 1 || r.Blocks[0][0].Frames[0] != "com/mod/Handler" {
		t.Fatalf("got %+v", r.Blocks)
	}
	if r.first != "10:00:00" {
		t.Errorf("first line time %q", r.first)
	}
}

func TestStartTime(t *testing.T) {
	end := time.Date(2026, 10, 9, 1, 30, 0, 0, time.Local)
	if got := startTime("23:10:05", end); !got.Equal(time.Date(2026, 10, 8, 23, 10, 5, 0, time.Local)) {
		t.Errorf("across midnight: %v", got)
	}
	if got := startTime("00:49:09", end); !got.Equal(time.Date(2026, 10, 9, 0, 49, 9, 0, time.Local)) {
		t.Errorf("same day: %v", got)
	}
}
