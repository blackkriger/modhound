package crash

import "testing"

const sample = `---- Minecraft Crash Report ----
Time: 25.09.26 4:09
Description: There was a severe problem during mod loading that has caused the game to fail

cpw.mods.fml.common.LoaderException: java.lang.NoSuchMethodError: com.aa.mod.items.botaniaSacrifice.setEnergyUsed(I)V
	at cpw.mods.fml.common.LoadController.transition(LoadController.java:163)
Caused by: java.lang.NoSuchMethodError: com.aa.mod.items.botaniaSacrifice.setEnergyUsed(I)V
	at com.aa.mod.items.botaniaSacrifice.<init>(botaniaSacrifice.java:49)
	at com.aa.mod.init.itemRegist.Register(itemRegist.java:68)
	... 20 more


A detailed walkthrough of the error, its code path and all known details is as follows:
---------------------------------------------------------------------------------------
-- Head --
Stacktrace:
	at net.minecraft.Other.x(Other.java:1)
`

func TestParseRootCause(t *testing.T) {
	r := Parse(sample)
	if len(r.Causes) != 2 {
		t.Fatalf("got %d causes", len(r.Causes))
	}
	c := r.Causes[1]
	if c.Short() != "NoSuchMethodError" || len(c.Frames) != 2 || c.Frames[0] != "com/aa/mod/items/botaniaSacrifice" {
		t.Fatalf("got %+v", c)
	}
	m, ok := c.Missing()
	if !ok || m.Owner != "com/aa/mod/items/botaniaSacrifice" || m.Name != "setEnergyUsed" {
		t.Fatalf("missing %+v", m)
	}
}

func TestMissingFieldAndClass(t *testing.T) {
	if m, _ := (&Cause{Error: "java.lang.NoSuchFieldError", Message: "xCoord"}).Missing(); m.Owner != "" || m.Name != "xCoord" {
		t.Fatalf("field %+v", m)
	}
	if m, _ := (&Cause{Error: "java.lang.NoClassDefFoundError", Message: "xyz/wagyourtail/jvmdg/j9/Stub"}).Missing(); m.Owner != "xyz/wagyourtail/jvmdg/j9/Stub" {
		t.Fatalf("class %+v", m)
	}
}

func TestMissingModernMessages(t *testing.T) {
	m, ok := (&Cause{Error: "java.lang.NoSuchMethodError", Message: "'void com.aa.Sacrifice.setEnergyUsed(int)'"}).Missing()
	if !ok || m.Owner != "com/aa/Sacrifice" || m.Name != "setEnergyUsed" {
		t.Fatalf("method %+v", m)
	}
	m, ok = (&Cause{Error: "java.lang.NoSuchFieldError", Message: "Class wayoftime.Int3 does not have member field 'int xCoord'"}).Missing()
	if !ok || m.Owner != "wayoftime/Int3" || m.Name != "xCoord" {
		t.Fatalf("field %+v", m)
	}
}

func TestMissingSkipsInitFailureAndParsesAbstractMethod(t *testing.T) {
	if _, ok := (&Cause{Error: "java.lang.NoClassDefFoundError", Message: "Could not initialize class com.foo.Bar"}).Missing(); ok {
		t.Fatal("static init failure is not a missing class")
	}
	m, ok := (&Cause{Error: "java.lang.AbstractMethodError", Message: "Receiver class com.x.Impl does not define or inherit an implementation of the resolved method 'abstract void tick(int)' of interface com.y.Api."}).Missing()
	if !ok || m.Owner != "com/y/Api" || m.Name != "tick" {
		t.Fatalf("abstract method %+v", m)
	}
}
