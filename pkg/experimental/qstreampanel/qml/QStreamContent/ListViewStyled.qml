import QtQuick 2.15
import QtQuick.Controls 2.15

ListView {
    id: listView

    header: TextField {
        width: listView.width
        placeholderText: "Filter"
    }

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
    delegate: Rectangle {
        id: rectangle
        visible: !headerItem.text || _text.text.toLowerCase().includes(headerItem.text.toLowerCase())
        width: parent.width
        height: visible ? 30 : 0
        color: listView.currentIndex
               == index ? listView.palette.highlight : (index % 2
                                               == 0 ? listView.palette.base : listView.palette.alternateBase)
        border {
            color: listView.palette.alternateBase
            width: 2
        }
        Row {
            spacing: 5
            x: 10
            width: parent.width - 10
            height: parent.height
            Text {
                id: _text
                width: parent.width
                height: parent.height
                text: name
                verticalAlignment: Text.AlignVCenter
                color: listView.currentIndex
                       == index ? listView.palette.highlightedText : listView.palette.text
                font.bold: listView.currentIndex == index
            }
        }
        MouseArea {
            width: parent.width
            height: parent.height
            onClicked: listView.currentIndex = index
        }
    }
}
