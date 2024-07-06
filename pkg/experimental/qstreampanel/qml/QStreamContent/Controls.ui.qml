

/*
This is a UI file (.ui.qml) that is intended to be edited in Qt Design Studio only.
It is supposed to be strictly declarative and only uses a subset of QML. If you edit
this file manually, you might introduce QML code that is not supported by Qt Design Studio.
Check out https://doc.qt.io/qtcreator/creator-quick-ui-forms.html for details on .ui.qml files.
*/
import QtQuick
import QtQuick.Controls
import QStream

Column {
    id: root
    Row {
        id: top_buttons
        width: 200
        height: 50
        anchors.left: parent.left
        anchors.right: parent.right

        Text {
            id: label_profile
            text: qsTr("Profile:")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            verticalAlignment: Text.AlignVCenter
            padding: 10
            font.pointSize: 16
        }

        Button {
            id: button_profile_create
            width: button_profile_create.height
            height: 100
            text: qsTr("+")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            icon.source: "../3rdparty/font-awesome/svgs/solid/plus.svg"
            display: AbstractButton.IconOnly
        }

        Button {
            id: button_profile_copy
            width: button_profile_copy.height
            text: qsTr("Copy")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            display: AbstractButton.IconOnly
            icon.source: "../3rdparty/font-awesome/svgs/solid/copy.svg"
        }

        Button {
            id: button_profile_edit
            width: button_profile_edit.height
            text: qsTr("Edit")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            display: AbstractButton.IconOnly
            icon.source: "../3rdparty/font-awesome/svgs/solid/pencil.svg"
        }

        Button {
            id: button_profile_delete
            width: button_profile_delete.height
            text: qsTr("-")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            display: AbstractButton.IconOnly
            icon.source: "../3rdparty/font-awesome/svgs/solid/trash-can.svg"
        }
    }

    ListViewStyled {
        id: listView_profiles
        width: parent.width
        height: parent.height - top_buttons.height - textField_title.height
                - scrollView_description.height - controls.height
        model: ListModel {
            ListElement {
                name: "Red"
            }

            ListElement {
                name: "Green"
            }

            ListElement {
                name: "Blue"
            }

            ListElement {
                name: "White"
            }
        }
    }

    TextField {
        id: textField_title
        width: parent.width
        placeholderText: "Stream title"
    }

    ScrollView {
        id: scrollView_description
        anchors.left: parent.left
        anchors.right: parent.right
        height: Math.min(contentHeight, 100)
        contentWidth: width
        contentHeight: textArea_description.implicitHeight

        TextArea {
            id: textArea_description
            wrapMode: Text.WordWrap
            placeholderText: qsTr("Stream description")
        }
    }
    Row {
        id: controls
        width: 200
        height: top_buttons.height
        anchors.left: parent.left
        anchors.right: parent.right

        CheckBox {
            id: checkbox_twitch
            text: qsTr("Twtich")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            checked: true
        }

        CheckBox {
            id: checkbox_youtube
            text: qsTr("YouTube")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            checked: true
        }

        DelayButton {
            id: button_setupStream
            width: (controls.width - checkbox_twitch.width - checkbox_youtube.width) / 2
            text: qsTr("Setup stream")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            delay: 1000
        }

        DelayButton {
            id: button_startStream
            width: button_setupStream.width
            text: qsTr("Start stream")
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            display: AbstractButton.IconOnly
            icon.height: 24
            icon.width: 24
            icon.source: "../3rdparty/font-awesome/svgs/solid/circle-play.svg"
            delay: 5000
            palette {
                dark: '#0f0'
            }
        }
    }

    Item {
        id: __materialLibrary__
    }
}
