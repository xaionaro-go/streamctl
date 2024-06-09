
streampanel-android:
	cd cmd/streampanel && ANDROID_HOME=${HOME}/Android/Sdk fyne package -release -os android

streampanel-ios:
	cd cmd/streampanel && fyne package -release -os ios
