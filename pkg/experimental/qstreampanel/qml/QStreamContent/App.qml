import QtQuick 6.7
import QtQuick.Controls 2.15
import QStream

Window {
    width: Constants.width
    height: Constants.height

    visible: true

    TabBar {
        anchors.left: parent.left
        anchors.right: parent.right

        id: tabBar
        currentIndex: swipeView.currentIndex

        TabButton {
            text: qsTr("Control")
        }

        TabButton {
            text: qsTr("Monitor")
        }

        TabButton {
            text: qsTr("Settings")
        }
    }

    SwipeView {
        id: swipeView
        x: 0
        y: 40
        anchors.top: tabBar.bottom
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        anchors.left: parent.left
        currentIndex: tabBar.currentIndex

        Controls {
            id: control
        }

        Settings {
            id: settings
        }

        Monitor {
            id: monitor
        }
    }

}
