# The Go core calls back into Java by name over JNI, which R8 cannot see.
# Everything gomobile generated, and every implementation of its interfaces,
# has to survive shrinking under its original name.
-keep class go.** { *; }
-keep class io.github.tanmayanand.carta.core.** { *; }
-keep class * implements io.github.tanmayanand.carta.core.Progress { *; }
