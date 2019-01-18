// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

import { APP_STATE_MUTATIONS } from '../mutationConstants';

export const appStateModule = {
	state: {
		// Object that contains all states of views
		appState: {
            isAddTeamMembersPopupShown: false,
			isNewProjectPopupShown: false,
            isProjectsDropdownShown: false,
            isAccountDropdownShown: false,
            isDeleteProjectPopupShown: false,
            isDeleteAccountPopupShown: false,
            isSortProjectMembersByPopupShown: false,
        },
	},
	mutations: {
		// Mutation changing add team members popup visibility
        [APP_STATE_MUTATIONS.TOGGLE_ADD_TEAMMEMBER_POPUP](state: any): void {
            state.appState.isAddTeamMembersPopupShown = !state.appState.isAddTeamMembersPopupShown;
        },

        // Mutation changing new project popup visibility
        [APP_STATE_MUTATIONS.TOGGLE_NEW_PROJECT_POPUP](state: any): void {
            state.appState.isNewProjectPopupShown = !state.appState.isNewProjectPopupShown;
        },

        // Mutation changing project dropdown visibility
        [APP_STATE_MUTATIONS.TOGGLE_PROJECT_DROPDOWN](state: any): void {
            state.appState.isProjectsDropdownShown = !state.appState.isProjectsDropdownShown;
        },

        // Mutation changing account dropdown visibility
        [APP_STATE_MUTATIONS.TOGGLE_ACCOUNT_DROPDOWN](state: any): void {
            state.appState.isAccountDropdownShown = !state.appState.isAccountDropdownShown;
        },

        // Mutation changing delete project popup visibility
        [APP_STATE_MUTATIONS.TOGGLE_DELETE_PROJECT_DROPDOWN](state: any): void {
            state.appState.isDeleteProjectPopupShown = !state.appState.isDeleteProjectPopupShown;
        },

        // Mutation changing delete account popup visibility
        [APP_STATE_MUTATIONS.TOGGLE_DELETE_ACCOUNT_DROPDOWN](state: any): void {
            state.appState.isDeleteAccountPopupShown = !state.appState.isDeleteAccountPopupShown;
        },
		// Mutation changing 'sort project members by' popup visibility
		[APP_STATE_MUTATIONS.TOGGLE_SORT_PM_BY_DROPDOWN](state: any): void {
			state.appState.isSortProjectMembersByPopupShown = !state.appState.isSortProjectMembersByPopupShown;
		},

        // Mutation that closes each popup/dropdown
        [APP_STATE_MUTATIONS.CLOSE_ALL](state: any): void {
            state.appState.isAddTeamMembersPopupShown = false;
            state.appState.isNewProjectPopupShown = false;
            state.appState.isProjectsDropdownShown = false;
            state.appState.isAccountDropdownShown = false;
            state.appState.isDeleteProjectPopupShown = false;
            state.appState.isDeleteAccountPopupShown = false;
            state.appState.isSortProjectMembersByPopupShown = false;
        },
	},
	actions: {
		// Commits mutation for changing app popups and dropdowns visibility state
        toggleAddTeamMembersPopup: function ({commit, state}: any): void {
            if (!state.appState.isAddTeamMembersPopupShown) {
                commit(APP_STATE_MUTATIONS.CLOSE_ALL);
            }

            commit(APP_STATE_MUTATIONS.TOGGLE_ADD_TEAMMEMBER_POPUP);
        },
        toggleNewProjectPopup: function ({commit, state}: any): void {
            if (!state.appState.isNewProjectPopupShown) {
                commit(APP_STATE_MUTATIONS.CLOSE_ALL);
            }

            commit(APP_STATE_MUTATIONS.TOGGLE_NEW_PROJECT_POPUP);
        },
        toggleProjectsDropdown: function ({commit, state}: any): void {
            if (!state.appState.isProjectsDropdownShown) {
                commit(APP_STATE_MUTATIONS.CLOSE_ALL);
            }

            commit(APP_STATE_MUTATIONS.TOGGLE_PROJECT_DROPDOWN);
        },
        toggleAccountDropdown: function ({commit, state}: any): void {
            if (!state.appState.isAccountDropdownShown) {
                commit(APP_STATE_MUTATIONS.CLOSE_ALL);
            }

            commit(APP_STATE_MUTATIONS.TOGGLE_ACCOUNT_DROPDOWN);
        },
        toggleDeleteProjectPopup: function ({commit, state}: any): void {
            if (!state.appState.isDeleteProjectPopupShown) {
                commit(APP_STATE_MUTATIONS.CLOSE_ALL);
            }

            commit(APP_STATE_MUTATIONS.TOGGLE_DELETE_PROJECT_DROPDOWN);
        },
        toggleDeleteAccountPopup: function ({commit, state}: any): void {
            if (!state.appState.isDeleteAccountPopupShown) {
                commit(APP_STATE_MUTATIONS.CLOSE_ALL);
            }

            commit(APP_STATE_MUTATIONS.TOGGLE_DELETE_ACCOUNT_DROPDOWN);
        },
		toggleSortProjectMembersByPopup: function ({commit, state}: any): void {
			if (!state.appState.isSortProjectMembersByPopupShown) {
				commit(APP_STATE_MUTATIONS.CLOSE_ALL);
			}

			commit(APP_STATE_MUTATIONS.TOGGLE_SORT_PM_BY_DROPDOWN);
		},
        closePopups: function ({commit}: any): void {
            commit(APP_STATE_MUTATIONS.CLOSE_ALL);
        },
	},
};
